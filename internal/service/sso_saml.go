package service

import (
	"bytes"
	"compress/flate"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// --------------------------------------------------------------------------
// SAML authentication
// --------------------------------------------------------------------------

// XML Digital Signature (XMLDSig) types — used to parse the embedded signature.
type dsSignature struct {
	SignedInfo     dsSignedInfo `xml:"SignedInfo"`
	SignatureValue string       `xml:"SignatureValue"`
}

type dsSignedInfo struct {
	CanonicalizationMethod dsAlgorithm   `xml:"CanonicalizationMethod"`
	SignatureMethod        dsAlgorithm   `xml:"SignatureMethod"`
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
	ID         string         `xml:"ID,attr"`
	Issuer     string         `xml:"Issuer"`
	Subject    samlSubject    `xml:"Subject"`
	Conditions samlConditions `xml:"Conditions"`
	AttrStmts  []samlAttrStmt `xml:"AttributeStatement"`
	Signature  *dsSignature   `xml:"Signature"`
}

type samlSubject struct {
	NameID               samlNameID                `xml:"NameID"`
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
	NotBefore           string                    `xml:"NotBefore,attr"`
	NotOnOrAfter        string                    `xml:"NotOnOrAfter,attr"`
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
		// An assertion with no AudienceRestriction must fail too: the bearer profile
		// requires one, and without it an assertion meant for any SP would be accepted.
		if !audienceOK {
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
				if bytes.Contains(xmlBytes[idx:min(idx+512, len(xmlBytes))], idAttr) {
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
// the SAMLRequest query parameter to the IdP SSO endpoint (sso_url).
// SAMLAuthnRequestURL builds the IdP redirect. forceAuthn asks the IdP to
// authenticate the user again rather than reuse its own session.
func (s *SSOService) SAMLAuthnRequestURL(ctx context.Context, relayState string, forceAuthn bool) (string, error) {
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
	force := ""
	if forceAuthn {
		force = `ForceAuthn="true" `
	}
	xmlStr := fmt.Sprintf(
		`<samlp:AuthnRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" `+
			`ID="%s" Version="2.0" IssueInstant="%s" %s`+
			`AssertionConsumerServiceURL="%s" `+
			`Destination="%s">`+
			`<saml:Issuer xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion">%s</saml:Issuer>`+
			`</samlp:AuthnRequest>`,
		id, issueInstant, force, acsURL, idpSSOURL, entityID,
	)

	// HTTP-Redirect binding: raw DEFLATE + Base64 + URL-encode (SAML spec section 3.4.4.1)
	var buf bytes.Buffer
	fw, err := flate.NewWriter(&buf, flate.BestCompression)
	if err != nil {
		return "", fmt.Errorf("saml authn request deflate init: %w", err)
	}
	if _, err := fw.Write([]byte(xmlStr)); err != nil {
		_ = fw.Close()
		return "", fmt.Errorf("saml authn request deflate write: %w", err)
	}
	if err := fw.Close(); err != nil {
		return "", fmt.Errorf("saml authn request deflate close: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())
	authnURL := idpSSOURL + "?SAMLRequest=" + url.QueryEscape(encoded)
	// The HTTP-Redirect binding caps RelayState at 80 bytes (SAML bindings 3.4.3);
	// a longer one is dropped so strict IdPs don't reject the whole sign-in.
	if relayState != "" && len(relayState) <= 80 {
		authnURL += "&RelayState=" + url.QueryEscape(relayState)
	}
	return authnURL, nil
}
