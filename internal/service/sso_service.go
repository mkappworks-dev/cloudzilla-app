package service

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mkappworks/cloudzilla/internal/config"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

// SSOService handles LDAP and SAML authentication flows.
type SSOService struct {
	store *store.SSOStore
	users *store.UserStore
	cfg   config.AuthConfig
}

// NewSSOService creates a new SSOService.
func NewSSOService(s *store.SSOStore, users *store.UserStore, cfg config.AuthConfig) *SSOService {
	return &SSOService{store: s, users: users, cfg: cfg}
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
	cfg, err := s.store.GetByProvider(ctx, "ldap")
	if err != nil || !cfg.Enabled {
		return nil, "", fmt.Errorf("ldap sso not configured or disabled")
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

	bindDN := fmt.Sprintf(bindDNTmpl, username)
	addr := net.JoinHostPort(host, port)

	useTLS := cfg.Config[model.LDAPKeyUseTLS] == "true"
	if err := bindLDAP(addr, bindDN, password, useTLS); err != nil {
		return nil, "", fmt.Errorf("ldap authentication failed: %w", err)
	}

	// Derive a stable SSO ID (use the bind DN as the canonical unique identifier)
	ssoID := bindDN

	return s.findOrProvisionUser(ctx, "ldap", ssoID, username, username+"@ldap.local")
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
	// Read tag + length prefix
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	length := int(header[1])
	var extra int
	if length&0x80 != 0 {
		numBytes := length & 0x7f
		if numBytes > 4 {
			return nil, fmt.Errorf("ldap response length too large")
		}
		lenBuf := make([]byte, numBytes)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return nil, fmt.Errorf("read length bytes: %w", err)
		}
		extra = numBytes
		length = 0
		for _, b := range lenBuf {
			length = length<<8 | int(b)
		}
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	result := make([]byte, 2+extra+length)
	copy(result, header)
	copy(result[2+extra:], body)
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
	Issuer    string         `xml:"Issuer"`
	Subject   samlSubject    `xml:"Subject"`
	Conditions samlConditions `xml:"Conditions"`
	AttrStmts []samlAttrStmt `xml:"AttributeStatement"`
}

type samlSubject struct {
	NameID samlNameID `xml:"NameID"`
}

type samlNameID struct {
	Value string `xml:",chardata"`
}

type samlConditions struct {
	NotBefore    string `xml:"NotBefore,attr"`
	NotOnOrAfter string `xml:"NotOnOrAfter,attr"`
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
	cfg, err := s.store.GetByProvider(ctx, "saml")
	if err != nil || !cfg.Enabled {
		return nil, "", fmt.Errorf("saml sso not configured or disabled")
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

	// Validate conditions: NotBefore / NotOnOrAfter
	const samlTimeFmt = "2006-01-02T15:04:05Z"
	now := time.Now().UTC()
	if nb := resp.Assertion.Conditions.NotBefore; nb != "" {
		t, err := time.Parse(samlTimeFmt, nb)
		if err == nil && now.Before(t.Add(-30*time.Second)) {
			return nil, "", fmt.Errorf("saml assertion not yet valid (NotBefore=%s)", nb)
		}
	}
	if na := resp.Assertion.Conditions.NotOnOrAfter; na != "" {
		t, err := time.Parse(samlTimeFmt, na)
		if err == nil && now.After(t.Add(30*time.Second)) {
			return nil, "", fmt.Errorf("saml assertion expired (NotOnOrAfter=%s)", na)
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

	return s.findOrProvisionUser(ctx, "saml", nameID, username, email)
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

// --------------------------------------------------------------------------
// User provisioning
// --------------------------------------------------------------------------

// findOrProvisionUser looks up a user by SSO provider + ID, or creates one.
// Returns the user and a signed JWT token.
func (s *SSOService) findOrProvisionUser(ctx context.Context, provider, ssoID, username, email string) (*model.User, string, error) {
	// Try existing SSO-linked account
	u, err := s.store.GetUserBySSO(ctx, provider, ssoID)
	if err == nil {
		token, err := s.generateJWT(u)
		return u, token, err
	}

	// Try existing account by email
	u, err = s.users.GetByEmailWithRole(ctx, email)
	if err == nil {
		// Link this account to the SSO provider
		if _, linkErr := s.users.DB().ExecContext(ctx,
			`UPDATE users SET sso_provider = $1, sso_id = $2, updated_at = NOW() WHERE id = $3`,
			provider, ssoID, u.ID,
		); linkErr != nil {
			return nil, "", fmt.Errorf("link sso to existing user: %w", linkErr)
		}
		token, err := s.generateJWT(u)
		return u, token, err
	}

	// Provision new user
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
  <md:SPSSODescriptor AuthnRequestsSigned="false" WantAssertionsSigned="false"
      protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <md:AssertionConsumerService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST"
        Location="%s" index="1"/>
  </md:SPSSODescriptor>
</md:EntityDescriptor>`, entityID, acsURL), nil
}

// SAMLAuthnRequestURL builds an unsolicited AuthnRequest redirect URL.
// For simplicity this uses HTTP-Redirect binding with an unsigned request,
// which most SAML IdPs support in test environments.
func (s *SSOService) SAMLAuthnRequestURL(ctx context.Context) (string, error) {
	cfg, err := s.store.GetByProvider(ctx, "saml")
	if err != nil || !cfg.Enabled {
		return "", fmt.Errorf("saml not configured or disabled")
	}
	metadataURL := cfg.Config[model.SAMLKeyMetadataURL]
	acsURL := cfg.Config[model.SAMLKeyACSURL]
	entityID := cfg.Config[model.SAMLKeyEntityID]

	// Build minimal AuthnRequest XML
	id := fmt.Sprintf("id%d", time.Now().UnixNano())
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_ = fmt.Sprintf(
		`<samlp:AuthnRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" `+
			`ID="%s" Version="2.0" IssueInstant="%s" `+
			`AssertionConsumerServiceURL="%s" `+
			`Destination="%s">`+
			`<saml:Issuer xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion">%s</saml:Issuer>`+
			`</samlp:AuthnRequest>`,
		id, now, acsURL, metadataURL, entityID,
	)
	// In a real deployment you would parse IdP metadata to get the SSO URL.
	// Here we treat metadata_url as the IdP SSO endpoint directly.
	return metadataURL, nil
}
