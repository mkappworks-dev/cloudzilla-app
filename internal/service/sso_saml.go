package service

import (
	"bytes"
	"compress/flate"
	"context"
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

	"github.com/beevik/etree"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	dsig "github.com/russellhaering/goxmldsig"
	"github.com/russellhaering/goxmldsig/etreeutils"
)

// --------------------------------------------------------------------------
// SAML authentication
// --------------------------------------------------------------------------

const samlAssertionNS = "urn:oasis:names:tc:SAML:2.0:assertion"

// samlResponse is the minimal XML structure we need to parse.
type samlResponse struct {
	XMLName xml.Name   `xml:"Response"`
	Issuer  string     `xml:"Issuer"`
	Status  samlStatus `xml:"Status"`
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

	now := s.clock().UTC()
	assertion, err := verifySAMLAssertion(xmlBytes, idpCert, now)
	if err != nil {
		return nil, "", fmt.Errorf("saml signature verification: %w", err)
	}

	// Validate conditions: NotBefore / NotOnOrAfter.
	// SAML 2.0 allows fractional seconds (e.g. Azure AD, Okta, Google all emit them),
	// so try RFC3339Nano first and fall back to second-precision RFC3339.
	var assertionExpiry time.Time
	if nb := assertion.Conditions.NotBefore; nb != "" {
		t, err := parseSAMLTime(nb)
		if err != nil {
			return nil, "", fmt.Errorf("saml assertion NotBefore unparseable: %w", err)
		}
		if now.Before(t.Add(-30 * time.Second)) {
			return nil, "", fmt.Errorf("saml assertion not yet valid (NotBefore=%s)", nb)
		}
	}
	if na := assertion.Conditions.NotOnOrAfter; na != "" {
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
	assertionID := assertion.ID
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
		for _, ar := range assertion.Conditions.AudienceRestriction {
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
		for _, sc := range assertion.Subject.SubjectConfirmations {
			if sc.Data.Recipient != "" && sc.Data.Recipient != expectedACSURL {
				return nil, "", fmt.Errorf("saml assertion recipient mismatch: got %q, want %q", sc.Data.Recipient, expectedACSURL)
			}
		}
	}

	nameID := strings.TrimSpace(assertion.Subject.NameID.Value)
	if nameID == "" {
		return nil, "", fmt.Errorf("saml assertion missing NameID")
	}

	// Extract email attribute (try common attribute names)
	email := extractSAMLAttribute(assertion.AttrStmts,
		"email", "mail", "emailAddress",
		"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress",
	)
	username := extractSAMLAttribute(assertion.AttrStmts,
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

// verifySAMLAssertion returns the response's assertion only if the IdP signed it.
//
// Claims must be read from the returned value alone: goxmldsig hands back the bytes
// it digested, so a second parse of the raw response (encoding/xml keeps the last of
// several assertions) can't substitute other content.
func verifySAMLAssertion(xmlBytes []byte, cert *x509.Certificate, now time.Time) (*samlAssertion, error) {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(xmlBytes); err != nil {
		return nil, fmt.Errorf("parse saml response: %w", err)
	}
	root := doc.Root()
	if root == nil {
		return nil, errors.New("parse saml response: no root element")
	}

	// Policy rather than the security boundary: Validate only checks the element it is
	// handed, so these refuse responses that would be ambiguous about which one that is.
	found := root.FindElements(".//Assertion")
	switch {
	case len(found) == 0:
		return nil, errors.New("saml response missing assertion")
	case len(found) > 1:
		return nil, errors.New("saml response must carry exactly one assertion")
	case found[0].Parent() != root:
		return nil, errors.New("saml assertion must be a direct child of the response")
	}
	if err := checkSAMLSignaturePolicy(found[0]); err != nil {
		return nil, err
	}

	// A plain FindElement would lose the declarations the Response made for the
	// assertion's prefix and fail with "undeclared namespace prefix".
	assertionEl, err := etreeutils.NSSelectOne(root, samlAssertionNS, "Assertion")
	if err != nil {
		return nil, err
	}
	if assertionEl == nil {
		return nil, errors.New("saml response missing assertion")
	}

	vctx := dsig.NewDefaultValidationContext(&dsig.MemoryX509CertificateStore{Roots: []*x509.Certificate{cert}})
	vctx.Clock = dsig.NewFakeClockAt(now)
	verified, err := vctx.Validate(assertionEl)
	if err != nil {
		return nil, err
	}

	out := etree.NewDocument()
	out.SetRoot(verified)
	verifiedBytes, err := out.WriteToBytes()
	if err != nil {
		return nil, err
	}
	var assertion samlAssertion
	if err := xml.Unmarshal(verifiedBytes, &assertion); err != nil {
		return nil, fmt.Errorf("parse saml assertion: %w", err)
	}
	return &assertion, nil
}

// checkSAMLSignaturePolicy refuses what goxmldsig accepts but a SAML assertion
// shouldn't use: a Reference to the whole document (URI ""), one to anything but
// this assertion's ID, several of either, and SHA-1.
func checkSAMLSignaturePolicy(assertion *etree.Element) error {
	id := assertion.SelectAttrValue("ID", "")
	if id == "" {
		return errors.New("saml assertion missing ID attribute")
	}
	sigs := assertion.SelectElements("Signature")
	switch {
	case len(sigs) == 0:
		return errors.New("saml assertion has no embedded signature")
	case len(sigs) > 1:
		return errors.New("saml assertion must carry exactly one signature")
	}
	signedInfo := sigs[0].SelectElement("SignedInfo")
	if signedInfo == nil {
		return errors.New("saml signature has no SignedInfo")
	}
	refs := signedInfo.SelectElements("Reference")
	if len(refs) != 1 {
		return errors.New("saml signature must have exactly one reference")
	}
	if uri := refs[0].SelectAttrValue("URI", ""); uri != "#"+id {
		return fmt.Errorf("saml signature reference %q is not the assertion %q", uri, "#"+id)
	}
	for _, method := range []*etree.Element{signedInfo.SelectElement("SignatureMethod"), refs[0].SelectElement("DigestMethod")} {
		if method != nil && strings.HasSuffix(strings.ToLower(method.SelectAttrValue("Algorithm", "")), "sha1") {
			return errors.New("saml signature uses SHA-1")
		}
	}
	return nil
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
