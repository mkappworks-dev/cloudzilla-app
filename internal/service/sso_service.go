package service

import (
	"bytes"
	"compress/flate"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// SSOService handles LDAP and SAML authentication flows.
// SSOService manages LDAP and SAML SSO authentication flows.
type SSOService struct {
	store       *store.SSOStore
	users       *store.UserStore
	cfg         config.AuthConfig
	siteSetting *SiteSettingService
}

// NewSSOService creates a new SSOService.
// NewSSOService creates an SSOService with the given stores, auth config, and site settings.
func NewSSOService(s *store.SSOStore, users *store.UserStore, cfg config.AuthConfig, siteSetting *SiteSettingService) *SSOService {
	return &SSOService{store: s, users: users, cfg: cfg, siteSetting: siteSetting}
}

// GetConfig returns the SSOConfig for the given provider.
// Returns nil (no error) if no config has been saved yet.
func (s *SSOService) GetConfig(ctx context.Context, provider string) (*model.SSOConfig, error) {
	cfg, err := s.store.GetByProvider(ctx, provider)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return cfg, err
}

// ListConfigs returns all stored SSO configs.
func (s *SSOService) ListConfigs(ctx context.Context) ([]*model.SSOConfig, error) {
	return s.store.ListAll(ctx)
}

// SetConfig creates or replaces the configuration for a provider.
func (s *SSOService) SetConfig(ctx context.Context, provider string, config map[string]string, enabled bool) error {
	if provider != "ldap" && provider != "saml" {
		return fmt.Errorf("unknown sso provider: %s", provider)
	}
	return s.store.Upsert(ctx, provider, config, enabled)
}

// --------------------------------------------------------------------------
// LDAP authentication
// --------------------------------------------------------------------------

// AuthenticateLDAP authenticates a user via LDAP simple bind.
// It dials the configured LDAP server, sends an LDAPv3 BindRequest, and checks the response.
// On success it looks up or provisions the local user account and returns a JWT.
func (s *SSOService) AuthenticateLDAP(ctx context.Context, username, password string) (*model.User, string, error) {
	if !s.siteSetting.AllowLogin(ctx) {
		return nil, "", ErrLoginDisabled
	}

	cfg, err := s.store.GetByProvider(ctx, "ldap")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", fmt.Errorf("ldap sso not configured")
		}
		return nil, "", fmt.Errorf("ldap sso config lookup: %w", err)
	}
	if !cfg.Enabled {
		return nil, "", fmt.Errorf("ldap sso is disabled")
	}

	host := cfg.Config[model.LDAPKeyHost]
	port := cfg.Config[model.LDAPKeyPort]
	if port == "" {
		port = "389"
	}
	bindDNTmpl := cfg.Config[model.LDAPKeyBindDNTmpl]
	if host == "" || bindDNTmpl == "" {
		return nil, "", fmt.Errorf("ldap config incomplete: host and bind_dn_tmpl required")
	}

	// Escape the username per RFC 4514 before interpolating into the DN template
	// to prevent LDAP injection via DN special characters.
	bindDN := fmt.Sprintf(bindDNTmpl, escapeLDAPDN(username))
	addr := net.JoinHostPort(host, port)

	useTLS := cfg.Config[model.LDAPKeyUseTLS] == "true"
	if err := bindLDAP(addr, bindDN, password, useTLS); err != nil {
		return nil, "", fmt.Errorf("ldap authentication failed: %w", err)
	}

	// Derive a stable SSO ID (use the bind DN as the canonical unique identifier)
	ssoID := bindDN

	allowReg := s.siteSetting.AllowRegistration(ctx)
	return s.findOrProvisionUser(ctx, "ldap", ssoID, username, username+"@ldap.local", allowReg)
}

// escapeLDAPDN escapes a string for safe interpolation into an LDAP DN per RFC 4514.
// The following characters are escaped with a leading backslash: , = + < > # ; \ "
// Control characters and non-ASCII bytes are hex-escaped as \XX.
// Leading and trailing spaces are also escaped.
func escapeLDAPDN(s string) string {
	runes := []rune(s)
	var b strings.Builder
	for i, c := range runes {
		switch {
		case c == 0:
			b.WriteString("\\00")
		case c == '\\':
			b.WriteString("\\\\")
		case c == '"':
			b.WriteString("\\\"")
		case c == ',' || c == '=' || c == '+' || c == '<' || c == '>' || c == '#' || c == ';':
			b.WriteByte('\\')
			b.WriteRune(c)
		case c == ' ' && (i == 0 || i == len(runes)-1):
			b.WriteString("\\ ")
		case c < 0x20 || c == 0x7f:
			fmt.Fprintf(&b, "\\%02X", c)
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}

// bindLDAP performs an LDAPv3 simple bind.
// BER encoding for BindRequest (Application tag 0):
//
//	BindRequest ::= [APPLICATION 0] SEQUENCE {
//	    version   INTEGER (1..127),
//	    name      LDAPDN,
//	    authentication AuthenticationChoice }
//	AuthenticationChoice ::= CHOICE {
//	    simple  [0] OCTET STRING }
func bindLDAP(addr, dn, password string, useTLS bool) error {
	var conn net.Conn
	var err error

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if useTLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("ldap dial: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	req := encodeLDAPBindRequest(1, dn, password)
	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("ldap write: %w", err)
	}

	resp, err := readLDAPResponse(conn)
	if err != nil {
		return fmt.Errorf("ldap read: %w", err)
	}

	return parseLDAPBindResponse(resp)
}

// encodeLDAPBindRequest encodes an LDAPv3 BindRequest as a BER-encoded byte slice.
// The message ID is fixed at 1.
//
// LDAPMessage structure:
//
//	SEQUENCE {
//	    messageID  INTEGER,
//	    protocolOp BindRequest }
func encodeLDAPBindRequest(msgID int, dn, password string) []byte {
	// Encode BindRequest body: version(3), name(dn), authentication(simple=password)
	version := berEncodeInteger(3)
	name := berEncodeOctetString(dn)
	simpleAuth := berEncodeContextPrimitive(0, []byte(password)) // [0] OCTET STRING

	bindBody := append(version, name...)
	bindBody = append(bindBody, simpleAuth...)

	// Wrap in APPLICATION 0 (BindRequest)
	bindReq := berWrap(0x60, bindBody) // 0x60 = APPLICATION CONSTRUCTED 0

	// Wrap in LDAPMessage SEQUENCE
	msgIDBytes := berEncodeInteger(msgID)
	msgBody := append(msgIDBytes, bindReq...)
	return berWrap(0x30, msgBody) // 0x30 = UNIVERSAL CONSTRUCTED SEQUENCE
}

// berEncodeInteger encodes a small non-negative integer as BER INTEGER TLV.
func berEncodeInteger(n int) []byte {
	return berWrap(0x02, []byte{byte(n)})
}

// berEncodeOctetString encodes a UTF-8 string as BER OCTET STRING TLV.
func berEncodeOctetString(s string) []byte {
	return berWrap(0x04, []byte(s))
}

// berEncodeContextPrimitive encodes a context-specific primitive TLV (e.g. [0]).
func berEncodeContextPrimitive(tag byte, value []byte) []byte {
	return berWrap(0x80|tag, value) // 0x80 = context-specific primitive
}

// berWrap wraps a value with a BER tag and length.
func berWrap(tag byte, value []byte) []byte {
	l := len(value)
	var lenBytes []byte
	if l < 128 {
		lenBytes = []byte{byte(l)}
	} else if l < 256 {
		lenBytes = []byte{0x81, byte(l)}
	} else {
		lenBytes = []byte{0x82, byte(l >> 8), byte(l)}
	}
	result := []byte{tag}
	result = append(result, lenBytes...)
	result = append(result, value...)
	return result
}

// readLDAPResponse reads exactly one BER TLV from conn.
func readLDAPResponse(conn net.Conn) ([]byte, error) {
	// Read tag + first length byte.
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	length := int(header[1])
	// lenBuf holds the additional length bytes for long-form BER encoding.
	var lenBuf []byte
	if length&0x80 != 0 {
		numBytes := length & 0x7f
		if numBytes > 4 {
			return nil, fmt.Errorf("ldap response length too large")
		}
		lenBuf = make([]byte, numBytes)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return nil, fmt.Errorf("read length bytes: %w", err)
		}
		length = 0
		for _, b := range lenBuf {
			length = length<<8 | int(b)
		}
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	// Reconstruct the full TLV: tag byte + length indicator byte + (optional
	// multi-byte length value) + body. Previously lenBuf was left out of the
	// result, zeroing those bytes and corrupting the parser for responses >127 B.
	result := make([]byte, 0, 2+len(lenBuf)+length)
	result = append(result, header...)
	result = append(result, lenBuf...)
	result = append(result, body...)
	return result, nil
}

// parseLDAPBindResponse extracts the resultCode from an LDAPMessage BindResponse.
// A resultCode of 0 means success.
func parseLDAPBindResponse(data []byte) error {
	// data is: SEQUENCE { INTEGER(msgID), [APPLICATION 1] SEQUENCE { ENUM(resultCode), ... } }
	// We need to navigate: outer SEQUENCE body → skip msgID → APPLICATION 1 body → first ENUM byte
	if len(data) < 2 {
		return fmt.Errorf("ldap response too short")
	}
	// Skip outer SEQUENCE tag + length
	pos := 2
	if data[1]&0x80 != 0 {
		pos += int(data[1] & 0x7f)
	}
	if pos >= len(data) {
		return fmt.Errorf("ldap response truncated after outer sequence")
	}
	// Skip messageID: tag(0x02) + length + value
	if pos+2 > len(data) {
		return fmt.Errorf("ldap response truncated at messageID")
	}
	pos += 2 + int(data[pos+1]) // skip INTEGER TLV
	// Now at APPLICATION 1 (BindResponse = tag 0x61)
	if pos+2 > len(data) || data[pos] != 0x61 {
		return fmt.Errorf("ldap unexpected protocol op tag: %02x", data[pos])
	}
	pos += 2
	if data[pos-1]&0x80 != 0 {
		pos += int(data[pos-1] & 0x7f)
	}
	// resultCode is the first element: ENUM (0x0a) + len + value
	if pos+3 > len(data) || data[pos] != 0x0a {
		return fmt.Errorf("ldap missing resultCode")
	}
	resultCode := int(data[pos+2])
	if resultCode != 0 {
		return fmt.Errorf("ldap bind failed: resultCode=%d", resultCode)
	}
	return nil
}

// --------------------------------------------------------------------------
// SAML authentication
// --------------------------------------------------------------------------

// XML Digital Signature (XMLDSig) types — used to parse the embedded signature.
type dsSignature struct {
	SignedInfo      dsSignedInfo `xml:"SignedInfo"`
	SignatureValue  string       `xml:"SignatureValue"`
}

type dsSignedInfo struct {
	CanonicalizationMethod dsAlgorithm   `xml:"CanonicalizationMethod"`
	SignatureMethod         dsAlgorithm   `xml:"SignatureMethod"`
	Reference              []dsReference `xml:"Reference"`
}

type dsAlgorithm struct {
	Algorithm string `xml:"Algorithm,attr"`
}

type dsReference struct {
	URI          string      `xml:"URI,attr"`
	DigestMethod dsAlgorithm `xml:"DigestMethod"`
	DigestValue  string      `xml:"DigestValue"`
}

// samlResponse is the minimal XML structure we need to parse.
type samlResponse struct {
	XMLName   xml.Name       `xml:"Response"`
	Issuer    string         `xml:"Issuer"`
	Status    samlStatus     `xml:"Status"`
	Assertion *samlAssertion `xml:"Assertion"`
}

type samlStatus struct {
	StatusCode samlStatusCode `xml:"StatusCode"`
}

type samlStatusCode struct {
	Value string `xml:"Value,attr"`
}

type samlAssertion struct {
	ID         string          `xml:"ID,attr"`
	Issuer     string          `xml:"Issuer"`
	Subject    samlSubject     `xml:"Subject"`
	Conditions samlConditions  `xml:"Conditions"`
	AttrStmts  []samlAttrStmt  `xml:"AttributeStatement"`
	Signature  *dsSignature    `xml:"Signature"`
}

type samlSubject struct {
	NameID               samlNameID               `xml:"NameID"`
	SubjectConfirmations []samlSubjectConfirmation `xml:"SubjectConfirmation"`
}

type samlNameID struct {
	Value string `xml:",chardata"`
}

type samlSubjectConfirmation struct {
	Method string                      `xml:"Method,attr"`
	Data   samlSubjectConfirmationData `xml:"SubjectConfirmationData"`
}

type samlSubjectConfirmationData struct {
	Recipient    string `xml:"Recipient,attr"`
	NotOnOrAfter string `xml:"NotOnOrAfter,attr"`
	InResponseTo string `xml:"InResponseTo,attr"`
}

type samlConditions struct {
	NotBefore           string                  `xml:"NotBefore,attr"`
	NotOnOrAfter        string                  `xml:"NotOnOrAfter,attr"`
	AudienceRestriction []samlAudienceRestriction `xml:"AudienceRestriction"`
}

type samlAudienceRestriction struct {
	Audiences []string `xml:"Audience"`
}

type samlAttrStmt struct {
	Attributes []samlAttribute `xml:"Attribute"`
}

type samlAttribute struct {
	Name   string   `xml:"Name,attr"`
	Values []string `xml:"AttributeValue"`
}

// HandleSAMLCallback parses and validates an incoming SAML response (base64-encoded XML).
// It extracts the NameID and email attribute, then finds or provisions the user.
func (s *SSOService) HandleSAMLCallback(ctx context.Context, samlResponseB64 string) (*model.User, string, error) {
	if !s.siteSetting.AllowLogin(ctx) {
		return nil, "", ErrLoginDisabled
	}

	cfg, err := s.store.GetByProvider(ctx, "saml")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", fmt.Errorf("saml sso not configured")
		}
		return nil, "", fmt.Errorf("saml sso config lookup: %w", err)
	}
	if !cfg.Enabled {
		return nil, "", fmt.Errorf("saml sso is disabled")
	}

	// Require the IdP certificate before processing any response. Without
	// signature verification, the assertion content cannot be trusted at all.
	idpCert, err := parseSAMLCert(cfg.Config[model.SAMLKeyCert])
	if err != nil {
		return nil, "", fmt.Errorf("saml: idp_cert required for signature verification: %w", err)
	}

	xmlBytes, err := base64.StdEncoding.DecodeString(samlResponseB64)
	if err != nil {
		return nil, "", fmt.Errorf("decode saml response: %w", err)
	}

	var resp samlResponse
	if err := xml.Unmarshal(xmlBytes, &resp); err != nil {
		return nil, "", fmt.Errorf("parse saml response: %w", err)
	}

	// Validate status
	if !strings.HasSuffix(resp.Status.StatusCode.Value, ":Success") {
		return nil, "", fmt.Errorf("saml response status not success: %s", resp.Status.StatusCode.Value)
	}

	if resp.Assertion == nil {
		return nil, "", fmt.Errorf("saml response missing assertion")
	}

	// Verify the assertion's XML digital signature before trusting any content.
	if err := verifySAMLSignature(xmlBytes, resp.Assertion, idpCert); err != nil {
		return nil, "", fmt.Errorf("saml signature verification: %w", err)
	}

	// Validate conditions: NotBefore / NotOnOrAfter.
	// SAML 2.0 allows fractional seconds (e.g. Azure AD, Okta, Google all emit them),
	// so try RFC3339Nano first and fall back to second-precision RFC3339.
	now := time.Now().UTC()
	var assertionExpiry time.Time
	if nb := resp.Assertion.Conditions.NotBefore; nb != "" {
		t, err := parseSAMLTime(nb)
		if err != nil {
			return nil, "", fmt.Errorf("saml assertion NotBefore unparseable: %w", err)
		}
		if now.Before(t.Add(-30 * time.Second)) {
			return nil, "", fmt.Errorf("saml assertion not yet valid (NotBefore=%s)", nb)
		}
	}
	if na := resp.Assertion.Conditions.NotOnOrAfter; na != "" {
		t, err := parseSAMLTime(na)
		if err != nil {
			return nil, "", fmt.Errorf("saml assertion NotOnOrAfter unparseable: %w", err)
		}
		if now.After(t.Add(30 * time.Second)) {
			return nil, "", fmt.Errorf("saml assertion expired (NotOnOrAfter=%s)", na)
		}
		assertionExpiry = t
	}
	if assertionExpiry.IsZero() {
		// No NotOnOrAfter — bound the replay window to 5 minutes from now.
		assertionExpiry = now.Add(5 * time.Minute)
	}

	// Check assertion ID for replay: record this assertion ID so it cannot be
	// reused within the validity window.
	assertionID := resp.Assertion.ID
	if assertionID == "" {
		return nil, "", fmt.Errorf("saml assertion missing ID attribute")
	}
	used, err := s.store.IsAssertionUsed(ctx, assertionID)
	if err != nil {
		return nil, "", fmt.Errorf("saml replay check: %w", err)
	}
	if used {
		return nil, "", fmt.Errorf("saml assertion has already been used (replay detected)")
	}
	if err := s.store.MarkAssertionUsed(ctx, assertionID, assertionExpiry); err != nil {
		return nil, "", fmt.Errorf("saml replay record: %w", err)
	}

	// Validate Audience — the assertion must be intended for this SP entity ID.
	expectedEntityID := cfg.Config[model.SAMLKeyEntityID]
	if expectedEntityID != "" {
		audienceOK := false
		for _, ar := range resp.Assertion.Conditions.AudienceRestriction {
			for _, aud := range ar.Audiences {
				if aud == expectedEntityID {
					audienceOK = true
				}
			}
		}
		if !audienceOK && len(resp.Assertion.Conditions.AudienceRestriction) > 0 {
			return nil, "", fmt.Errorf("saml assertion audience does not match entity_id")
		}
	}

	// Validate Recipient — SubjectConfirmationData/@Recipient must match our ACS URL.
	expectedACSURL := cfg.Config[model.SAMLKeyACSURL]
	if expectedACSURL != "" {
		for _, sc := range resp.Assertion.Subject.SubjectConfirmations {
			if sc.Data.Recipient != "" && sc.Data.Recipient != expectedACSURL {
				return nil, "", fmt.Errorf("saml assertion recipient mismatch: got %q, want %q", sc.Data.Recipient, expectedACSURL)
			}
		}
	}

	nameID := strings.TrimSpace(resp.Assertion.Subject.NameID.Value)
	if nameID == "" {
		return nil, "", fmt.Errorf("saml assertion missing NameID")
	}

	// Extract email attribute (try common attribute names)
	email := extractSAMLAttribute(resp.Assertion.AttrStmts,
		"email", "mail", "emailAddress",
		"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress",
	)
	username := extractSAMLAttribute(resp.Assertion.AttrStmts,
		"uid", "username", "sAMAccountName",
		"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name",
	)
	if username == "" {
		username = nameID
	}
	if email == "" {
		email = nameID
	}

	allowReg := s.siteSetting.AllowRegistration(ctx)
	return s.findOrProvisionUser(ctx, "saml", nameID, username, email, allowReg)
}

// extractSAMLAttribute searches attribute statements for the first non-empty value
// matching any of the given attribute names (case-insensitive).
func extractSAMLAttribute(stmts []samlAttrStmt, names ...string) string {
	for _, stmt := range stmts {
		for _, attr := range stmt.Attributes {
			attrLower := strings.ToLower(attr.Name)
			for _, name := range names {
				if attrLower == strings.ToLower(name) {
					for _, v := range attr.Values {
						if v = strings.TrimSpace(v); v != "" {
							return v
						}
					}
				}
			}
		}
	}
	return ""
}

// parseSAMLTime parses a SAML timestamp, accepting both second-precision and
// fractional-second formats (RFC3339 / RFC3339Nano). Azure AD, Okta, and Google
// all emit fractional seconds, which the older fixed-format parser rejected.
func parseSAMLTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

// parseSAMLCert parses a PEM-encoded (or raw base64 DER) X.509 certificate
// stored in the SAML config's idp_cert field.
func parseSAMLCert(certStr string) (*x509.Certificate, error) {
	certStr = strings.TrimSpace(certStr)
	if certStr == "" {
		return nil, fmt.Errorf("idp_cert is not configured")
	}
	// Support raw base64 DER (no PEM headers) as well as full PEM.
	if !strings.Contains(certStr, "-----BEGIN") {
		certStr = "-----BEGIN CERTIFICATE-----\n" + certStr + "\n-----END CERTIFICATE-----"
	}
	block, _ := pem.Decode([]byte(certStr))
	if block == nil {
		return nil, fmt.Errorf("failed to decode idp_cert PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse idp_cert: %w", err)
	}
	return cert, nil
}

// verifySAMLSignature verifies the XML digital signature embedded in the SAML
// assertion against the configured IdP certificate.
//
// It supports RSA-SHA256 signatures (the overwhelmingly common SAML algorithm).
// The approach:
//  1. Locate the <ds:Signature> inside the assertion.
//  2. Extract the assertion element from the raw XML bytes (identified by the
//     assertion ID from the <ds:Reference URI="#id"> attribute).
//  3. Remove the embedded <ds:Signature> subtree to obtain the enveloped content.
//  4. Compute SHA-256 of that content and compare with DigestValue.
//  5. Verify the SignatureValue against the SignedInfo bytes using the cert.
//
// Note: this implementation does not perform full XML Exclusive Canonicalization
// (EXC-C14N). It verifies the digest and signature over the raw XML bytes,
// which works for the vast majority of real-world IdP responses but may reject
// responses that rely on namespace prefix rewriting by the C14N transform.
// For strict production use, replace this with a full SAML library.
func verifySAMLSignature(xmlBytes []byte, assertion *samlAssertion, cert *x509.Certificate) error {
	sig := assertion.Signature
	if sig == nil {
		return fmt.Errorf("saml assertion has no embedded signature")
	}

	// --- Step 1: locate the raw assertion element bytes ---
	// Find the opening tag that carries the assertion ID attribute.
	assertionID := assertion.ID
	if assertionID == "" {
		return fmt.Errorf("saml assertion missing ID attribute")
	}

	// Find the <Assertion ...> element boundaries in the raw XML.
	assertionStart := bytes.Index(xmlBytes, []byte("<"+assertionID))
	if assertionStart < 0 {
		// Try namespace-prefixed forms.
		for _, prefix := range []string{"saml:", "saml2:", "Assertion "} {
			idx := bytes.Index(xmlBytes, []byte("<"+prefix))
			if idx >= 0 {
				// Verify the ID attribute is present on this element.
				idAttr := []byte(`ID="` + assertionID + `"`)
				if bytes.Contains(xmlBytes[idx:idx+512], idAttr) {
					assertionStart = idx
					break
				}
			}
		}
	}
	if assertionStart < 0 {
		// Fall back: search by ID attribute value directly.
		idAttr := []byte(`ID="` + assertionID + `"`)
		pos := bytes.Index(xmlBytes, idAttr)
		if pos < 0 {
			return fmt.Errorf("saml: cannot locate assertion element with ID=%q in response", assertionID)
		}
		// Walk backwards to find the opening '<'.
		for assertionStart = pos; assertionStart > 0; assertionStart-- {
			if xmlBytes[assertionStart] == '<' {
				break
			}
		}
	}

	// Find the matching close tag by counting open/close depth.
	assertionBytes, err := extractXMLElement(xmlBytes[assertionStart:])
	if err != nil {
		return fmt.Errorf("saml: extract assertion element: %w", err)
	}

	// --- Step 2: apply enveloped-signature transform (remove <ds:Signature>) ---
	sigTagVariants := [][]byte{
		[]byte("<ds:Signature"),
		[]byte("<Signature"),
		[]byte("<dsig:Signature"),
	}
	contentForDigest := assertionBytes
	for _, tag := range sigTagVariants {
		if idx := bytes.Index(contentForDigest, tag); idx >= 0 {
			sigElement, err := extractXMLElement(contentForDigest[idx:])
			if err == nil {
				contentForDigest = append(
					append([]byte{}, contentForDigest[:idx]...),
					contentForDigest[idx+len(sigElement):]...,
				)
			}
			break
		}
	}

	// --- Step 3: verify digest ---
	if len(sig.SignedInfo.Reference) == 0 {
		return fmt.Errorf("saml signature has no references")
	}
	ref := sig.SignedInfo.Reference[0]
	digestBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(ref.DigestValue))
	if err != nil {
		return fmt.Errorf("saml: decode DigestValue: %w", err)
	}
	computed := sha256.Sum256(contentForDigest)
	if !bytes.Equal(computed[:], digestBytes) {
		return fmt.Errorf("saml assertion digest mismatch: signature does not match content")
	}

	// --- Step 4: verify the signature over SignedInfo ---
	sigValueBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sig.SignatureValue))
	if err != nil {
		return fmt.Errorf("saml: decode SignatureValue: %w", err)
	}

	// Extract the raw <ds:SignedInfo> bytes from the original XML rather than
	// re-marshalling the parsed Go struct. Re-marshalling produces different bytes
	// than what the IdP signed (different namespace declarations, attribute order,
	// whitespace), causing verification to always fail against real IdPs.
	// Note: for strict compliance, full XML Exclusive Canonicalization (EXC-C14N)
	// should be applied to these bytes before hashing; for a production deployment
	// replace this with a dedicated SAML library such as goxmldsig.
	var signedInfoBytes []byte
	for _, prefix := range []string{"<ds:SignedInfo", "<SignedInfo", "<dsig:SignedInfo"} {
		if idx := bytes.Index(xmlBytes, []byte(prefix)); idx >= 0 {
			elem, extractErr := extractXMLElement(xmlBytes[idx:])
			if extractErr == nil {
				signedInfoBytes = elem
				break
			}
		}
	}
	if signedInfoBytes == nil {
		return fmt.Errorf("saml: cannot locate SignedInfo element in response XML")
	}
	signedInfoHash := sha256.Sum256(signedInfoBytes)

	rsaKey, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("saml: idp_cert does not contain an RSA public key")
	}
	if err := rsa.VerifyPKCS1v15(rsaKey, crypto.SHA256, signedInfoHash[:], sigValueBytes); err != nil {
		return fmt.Errorf("saml assertion signature verification failed: %w", err)
	}
	return nil
}

// extractXMLElement extracts the first complete XML element from data,
// handling nested elements with the same tag name.
func extractXMLElement(data []byte) ([]byte, error) {
	if len(data) == 0 || data[0] != '<' {
		return nil, fmt.Errorf("data does not start with '<'")
	}
	// Extract the tag name.
	end := bytes.IndexAny(data[1:], " \t\n\r/>")
	if end < 0 {
		return nil, fmt.Errorf("malformed XML element")
	}
	tagName := string(data[1 : end+1])

	openTag := []byte("<" + tagName)
	closeTag := []byte("</" + tagName)

	depth := 0
	pos := 0
	for pos < len(data) {
		if bytes.HasPrefix(data[pos:], closeTag) {
			depth--
			if depth == 0 {
				// Find the '>' that closes this tag.
				gt := bytes.IndexByte(data[pos:], '>')
				if gt < 0 {
					return nil, fmt.Errorf("malformed closing tag")
				}
				return data[:pos+gt+1], nil
			}
			pos++
			continue
		}
		if bytes.HasPrefix(data[pos:], openTag) {
			// Check it's not already inside a self-closing tag.
			gt := bytes.IndexByte(data[pos:], '>')
			if gt > 0 && data[pos+gt-1] == '/' {
				pos += gt + 1
				continue
			}
			depth++
		}
		pos++
	}
	return nil, fmt.Errorf("unclosed XML element <%s>", tagName)
}

// --------------------------------------------------------------------------
// User provisioning
// --------------------------------------------------------------------------

// findOrProvisionUser looks up a user by SSO provider + ID, or creates one.
// Returns the user and a signed JWT token.
//
// Email-based auto-linking is intentionally absent: silently linking an SSO
// identity to an existing password account would let any IdP operator claim
// any user's account by asserting their email address. Users who want to
// connect SSO to an existing account must do so from their account settings.
func (s *SSOService) findOrProvisionUser(ctx context.Context, provider, ssoID, username, email string, allowRegistration bool) (*model.User, string, error) {
	// Return the existing SSO-linked account if one exists.
	u, err := s.store.GetUserBySSO(ctx, provider, ssoID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, "", fmt.Errorf("sso user lookup: %w", err)
	}
	if err == nil {
		token, err := s.generateJWT(u)
		return u, token, err
	}

	// If an account with this email already exists (password or OAuth login),
	// refuse to auto-link. The user must link SSO explicitly from their settings.
	_, emailErr := s.users.GetByEmailWithRole(ctx, email)
	if emailErr == nil {
		return nil, "", fmt.Errorf("an account with this email already exists; sign in with your password and link SSO from account settings")
	}
	if !errors.Is(emailErr, sql.ErrNoRows) {
		return nil, "", fmt.Errorf("sso email lookup: %w", emailErr)
	}

	// New user — enforce the site registration policy.
	if !allowRegistration {
		return nil, "", ErrRegistrationDisabled
	}

	safeUsername := sanitizeUsername(username)
	u, err = s.store.ProvisionSSOUser(ctx, safeUsername, email, provider, ssoID)
	if err != nil {
		return nil, "", fmt.Errorf("provision sso user: %w", err)
	}
	token, err := s.generateJWT(u)
	return u, token, err
}

// generateJWT produces a signed JWT for an authenticated user.
func (s *SSOService) generateJWT(u *model.User) (string, error) {
	claims := jwt.MapClaims{
		"sub":           u.ID,
		"username":      u.Username,
		"is_superadmin": u.IsSuperadmin,
		"exp":           time.Now().Add(s.cfg.JWTExpiry).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.cfg.JWTSecret))
}

// sanitizeUsername replaces non-alphanumeric characters with underscores and
// truncates to 39 characters (GitHub-style limit).
func sanitizeUsername(s string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(s) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			b.WriteRune(c)
		} else {
			b.WriteByte('_')
		}
	}
	result := strings.Trim(b.String(), "_-")
	if len(result) > 39 {
		result = result[:39]
	}
	if result == "" {
		result = "sso_user"
	}
	return result
}

// SAMLMetadataXML returns the service provider metadata XML for SAML discovery.
func (s *SSOService) SAMLMetadataXML(ctx context.Context) (string, error) {
	cfg, err := s.store.GetByProvider(ctx, "saml")
	if err != nil {
		return "", fmt.Errorf("no saml config: %w", err)
	}
	acsURL := cfg.Config[model.SAMLKeyACSURL]
	entityID := cfg.Config[model.SAMLKeyEntityID]
	return fmt.Sprintf(`<?xml version="1.0"?>
<md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata" entityID="%s">
  <md:SPSSODescriptor AuthnRequestsSigned="false" WantAssertionsSigned="true"
      protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <md:AssertionConsumerService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST"
        Location="%s" index="1"/>
  </md:SPSSODescriptor>
</md:EntityDescriptor>`, entityID, acsURL), nil
}

// SAMLAuthnRequestURL builds a SAML HTTP-Redirect binding AuthnRequest URL.
// The AuthnRequest XML is deflate-compressed, base64-encoded, and appended as
// the SAMLRequest query parameter to the IdP SSO endpoint (metadata_url).
func (s *SSOService) SAMLAuthnRequestURL(ctx context.Context) (string, error) {
	cfg, err := s.store.GetByProvider(ctx, "saml")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("saml sso not configured")
		}
		return "", fmt.Errorf("saml sso config lookup: %w", err)
	}
	if !cfg.Enabled {
		return "", fmt.Errorf("saml sso is disabled")
	}

	idpSSOURL := cfg.Config[model.SAMLKeySSOURL]
	acsURL := cfg.Config[model.SAMLKeyACSURL]
	entityID := cfg.Config[model.SAMLKeyEntityID]
	if idpSSOURL == "" || acsURL == "" || entityID == "" {
		return "", fmt.Errorf("saml config incomplete: sso_url, acs_url, and entity_id required")
	}

	id := fmt.Sprintf("id%d", time.Now().UnixNano())
	issueInstant := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	xmlStr := fmt.Sprintf(
		`<samlp:AuthnRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" `+
			`ID="%s" Version="2.0" IssueInstant="%s" `+
			`AssertionConsumerServiceURL="%s" `+
			`Destination="%s">`+
			`<saml:Issuer xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion">%s</saml:Issuer>`+
			`</samlp:AuthnRequest>`,
		id, issueInstant, acsURL, idpSSOURL, entityID,
	)

	// HTTP-Redirect binding: raw DEFLATE + Base64 + URL-encode (SAML spec section 3.4.4.1)
	var buf bytes.Buffer
	fw, err := flate.NewWriter(&buf, flate.BestCompression)
	if err != nil {
		return "", fmt.Errorf("saml authn request deflate init: %w", err)
	}
	if _, err := fw.Write([]byte(xmlStr)); err != nil {
		fw.Close()
		return "", fmt.Errorf("saml authn request deflate write: %w", err)
	}
	if err := fw.Close(); err != nil {
		return "", fmt.Errorf("saml authn request deflate close: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())
	return idpSSOURL + "?SAMLRequest=" + url.QueryEscape(encoded), nil
}
