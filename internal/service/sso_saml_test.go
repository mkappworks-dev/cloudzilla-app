package service

import (
	"bytes"
	"compress/flate"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const (
	samlSuccess = "urn:oasis:names:tc:SAML:2.0:status:Success"
	samlACS     = "https://cz.test.invalid/acs"
)

type samlSigner struct {
	key     *rsa.PrivateKey
	certPEM string
}

func newSAMLSigner() *samlSigner {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "idp.test.invalid"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	return &samlSigner{key: key, certPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))}
}

var (
	idpSigner   = sync.OnceValue(newSAMLSigner)
	otherSigner = sync.OnceValue(newSAMLSigner)
)

type samlSpec struct {
	ID, NameID, Audience, Recipient, Status string
	NotBefore, NotOnOrAfter                 string
	Attrs                                   map[string]string
	Unsigned                                bool
	NoAssertion                             bool
}

func validSAMLSpec(id string) samlSpec {
	return samlSpec{
		ID:           id,
		NameID:       "ann@idp.test",
		Audience:     "cz",
		Recipient:    samlACS,
		Status:       samlSuccess,
		NotBefore:    time.Now().UTC().Add(-time.Minute).Format(time.RFC3339),
		NotOnOrAfter: time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339),
		Attrs:        map[string]string{"email": "ann@example.test", "uid": "ann"},
	}
}

// buildSAMLResponse signs the way verifySAMLSignature checks: a digest over the
// assertion without its Signature, and an RSA-SHA256 signature over the raw SignedInfo.
func buildSAMLResponse(t *testing.T, signer *samlSigner, s samlSpec) string {
	t.Helper()
	const ds = `xmlns:ds="http://www.w3.org/2000/09/xmldsig#"`

	head := fmt.Sprintf(`<saml:Assertion xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="%s" Version="2.0" IssueInstant="2026-01-01T00:00:00Z"><saml:Issuer>https://idp.test.invalid</saml:Issuer>`, s.ID)

	var tail strings.Builder
	tail.WriteString(`<saml:Subject><saml:NameID>` + s.NameID + `</saml:NameID>`)
	if s.Recipient != "" {
		fmt.Fprintf(&tail, `<saml:SubjectConfirmation Method="urn:oasis:names:tc:SAML:2.0:cm:bearer"><saml:SubjectConfirmationData Recipient="%s"/></saml:SubjectConfirmation>`, s.Recipient)
	}
	tail.WriteString(`</saml:Subject>`)
	fmt.Fprintf(&tail, `<saml:Conditions NotBefore="%s" NotOnOrAfter="%s">`, s.NotBefore, s.NotOnOrAfter)
	if s.Audience != "" {
		tail.WriteString(`<saml:AudienceRestriction><saml:Audience>` + s.Audience + `</saml:Audience></saml:AudienceRestriction>`)
	}
	tail.WriteString(`</saml:Conditions>`)
	names := make([]string, 0, len(s.Attrs))
	for n := range s.Attrs {
		names = append(names, n)
	}
	sort.Strings(names)
	tail.WriteString(`<saml:AttributeStatement>`)
	for _, n := range names {
		fmt.Fprintf(&tail, `<saml:Attribute Name="%s"><saml:AttributeValue>%s</saml:AttributeValue></saml:Attribute>`, n, s.Attrs[n])
	}
	tail.WriteString(`</saml:AttributeStatement></saml:Assertion>`)

	sig := ""
	if !s.Unsigned {
		digest := sha256.Sum256([]byte(head + tail.String()))
		signedInfo := fmt.Sprintf(`<ds:SignedInfo %s><ds:CanonicalizationMethod Algorithm="http://www.w3.org/2001/10/xml-exc-c14n#"/><ds:SignatureMethod Algorithm="http://www.w3.org/2001/04/xmldsig-more#rsa-sha256"/><ds:Reference URI="#%s"><ds:DigestMethod Algorithm="http://www.w3.org/2001/04/xmlenc#sha256"/><ds:DigestValue>%s</ds:DigestValue></ds:Reference></ds:SignedInfo>`,
			ds, s.ID, base64.StdEncoding.EncodeToString(digest[:]))
		h := sha256.Sum256([]byte(signedInfo))
		raw, err := rsa.SignPKCS1v15(rand.Reader, signer.key, crypto.SHA256, h[:])
		if err != nil {
			t.Fatal(err)
		}
		sig = fmt.Sprintf(`<ds:Signature %s>%s<ds:SignatureValue>%s</ds:SignatureValue></ds:Signature>`, ds, signedInfo, base64.StdEncoding.EncodeToString(raw))
	}

	assertion := head + sig + tail.String()
	if s.NoAssertion {
		assertion = ""
	}
	return fmt.Sprintf(`<?xml version="1.0"?><samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" ID="_resp" Version="2.0"><samlp:Status><samlp:StatusCode Value="%s"/></samlp:Status>%s</samlp:Response>`, s.Status, assertion)
}

func saveSAML(t *testing.T, svc *SSOService, signer *samlSigner, enabled bool) {
	t.Helper()
	err := svc.SetConfig(context.Background(), "saml", map[string]string{
		model.SAMLKeyEntityID: "cz", model.SAMLKeySSOURL: "https://idp.test.invalid/sso",
		model.SAMLKeyACSURL: samlACS, model.SAMLKeyCert: signer.certPEM,
	}, enabled)
	if err != nil {
		t.Fatal(err)
	}
}

func callback(svc *SSOService, xml string) (*model.User, string, error) {
	return svc.HandleSAMLCallback(context.Background(), base64.StdEncoding.EncodeToString([]byte(xml)))
}

func TestHandleSAMLCallback_ProvisionsAndIssuesAToken(t *testing.T) {
	svc, db := newSSOTestService(t)
	saveSAML(t, svc, idpSigner(), true)

	u, token, err := callback(svc, buildSAMLResponse(t, idpSigner(), validSAMLSpec("_ok1")))
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != "ann" || u.Email != "ann@example.test" {
		t.Errorf("provisioned %q <%s>, want ann <ann@example.test>", u.Username, u.Email)
	}
	claims := jwt.MapClaims{}
	if _, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) { return []byte(ssoTestJWTSecret), nil }, jwt.WithValidMethods([]string{"HS256"})); err != nil {
		t.Fatalf("token does not verify: %v", err)
	}
	if claims["sub"] != float64(u.ID) || claims["username"] != "ann" {
		t.Errorf("claims = %v, want sub %d", claims, u.ID)
	}

	again, _, err := callback(svc, buildSAMLResponse(t, idpSigner(), validSAMLSpec("_ok2")))
	if err != nil || again.ID != u.ID {
		t.Errorf("returning sign-in: user %v, err %v; want user %d", again, err, u.ID)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE sso_provider = 'saml'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("saml users = %d (err %v), want 1", n, err)
	}
}

func TestHandleSAMLCallback_FallsBackToNameIDWithoutAttributes(t *testing.T) {
	svc, _ := newSSOTestService(t)
	saveSAML(t, svc, idpSigner(), true)
	spec := validSAMLSpec("_bare")
	spec.Attrs = nil
	spec.NameID = "  bob@idp.test  "

	u, _, err := callback(svc, buildSAMLResponse(t, idpSigner(), spec))
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "bob@idp.test" || u.Username != "bob_idp_test" {
		t.Errorf("provisioned %q <%s>", u.Username, u.Email)
	}
}

func TestHandleSAMLCallback_AcceptsFractionalSecondsAndSmallClockSkew(t *testing.T) {
	svc, _ := newSSOTestService(t)
	saveSAML(t, svc, idpSigner(), true)

	frac := validSAMLSpec("_frac")
	frac.NotBefore = time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	frac.NotOnOrAfter = time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)
	if _, _, err := callback(svc, buildSAMLResponse(t, idpSigner(), frac)); err != nil {
		t.Errorf("fractional seconds: %v", err)
	}

	skew := validSAMLSpec("_skew")
	skew.NotOnOrAfter = time.Now().UTC().Add(-10 * time.Second).Format(time.RFC3339)
	if _, _, err := callback(svc, buildSAMLResponse(t, idpSigner(), skew)); err != nil {
		t.Errorf("expired 10s ago, inside the 30s allowance: %v", err)
	}
}

func TestHandleSAMLCallback_Rejects(t *testing.T) {
	svc, db := newSSOTestService(t)
	saveSAML(t, svc, idpSigner(), true)
	_, email := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")

	tamper := func(old, new string) func(string) string {
		return func(x string) string { return strings.Replace(x, old, new, 1) }
	}
	for _, tc := range []struct {
		name    string
		signer  *samlSigner
		mod     func(*samlSpec)
		post    func(string) string
		wantErr string
	}{
		{"status not success", nil, func(s *samlSpec) { s.Status = "urn:oasis:names:tc:SAML:2.0:status:Responder" }, nil, "not success"},
		{"no assertion", nil, func(s *samlSpec) { s.NoAssertion = true }, nil, "missing assertion"},
		{"unsigned assertion", nil, func(s *samlSpec) { s.Unsigned = true }, nil, "no embedded signature"},
		{"signed by another key", otherSigner(), nil, nil, "signature verification failed"},
		{"name changed after signing", nil, nil, tamper("ann@idp.test", "admin@idp.test"), "digest mismatch"},
		{"conditions changed after signing", nil, nil, tamper("</saml:Conditions>", `</saml:Conditions><saml:Extra/>`), "digest mismatch"},
		{"expired", nil, func(s *samlSpec) { s.NotOnOrAfter = time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339) }, nil, "expired"},
		{"not yet valid", nil, func(s *samlSpec) { s.NotBefore = time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339) }, nil, "not yet valid"},
		{"unparseable NotBefore", nil, func(s *samlSpec) { s.NotBefore = "yesterday" }, nil, "NotBefore unparseable"},
		{"unparseable NotOnOrAfter", nil, func(s *samlSpec) { s.NotOnOrAfter = "tomorrow" }, nil, "NotOnOrAfter unparseable"},
		{"no assertion ID", nil, func(s *samlSpec) { s.ID = "" }, nil, "ID"},
		{"audience for another SP", nil, func(s *samlSpec) { s.Audience = "someone-else" }, nil, "audience"},
		{"no audience restriction", nil, func(s *samlSpec) { s.Audience = "" }, nil, "audience"},
		{"recipient is another ACS", nil, func(s *samlSpec) { s.Recipient = "https://evil.test.invalid/acs" }, nil, "recipient mismatch"},
		{"blank NameID", nil, func(s *samlSpec) { s.NameID = "   " }, nil, "missing NameID"},
		{"email already has a password account", nil, func(s *samlSpec) { s.Attrs = map[string]string{"email": email} }, nil, "already exists"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := validSAMLSpec("_rej_" + strings.ReplaceAll(tc.name, " ", "_"))
			if tc.mod != nil {
				tc.mod(&spec)
			}
			signer := tc.signer
			if signer == nil {
				signer = idpSigner()
			}
			xml := buildSAMLResponse(t, signer, spec)
			if tc.post != nil {
				xml = tc.post(xml)
			}
			u, token, err := callback(svc, xml)
			if err == nil || u != nil || token != "" {
				t.Fatalf("got user %v, token issued = %v, err %v; want a refusal", u, token != "", err)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE sso_provider = 'saml'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("rejected responses provisioned %d saml users (err %v)", n, err)
	}
}

func TestHandleSAMLCallback_RejectsMalformedInput(t *testing.T) {
	svc, _ := newSSOTestService(t)
	saveSAML(t, svc, idpSigner(), true)
	ctx := context.Background()

	if _, _, err := svc.HandleSAMLCallback(ctx, "%%% not base64"); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Errorf("non-base64: got %v", err)
	}
	if _, _, err := callback(svc, "this is not xml"); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Errorf("non-XML: got %v", err)
	}
	if _, _, err := callback(svc, ""); err == nil {
		t.Error("empty response accepted")
	}
}

// The response is attacker-supplied; a short one must fail verification, not crash it.
func TestHandleSAMLCallback_TinySignedLookingResponseDoesNotPanic(t *testing.T) {
	svc, _ := newSSOTestService(t)
	saveSAML(t, svc, idpSigner(), true)
	tiny := `<Response><Status><StatusCode Value="x:Success"/></Status><saml:Assertion ID="a"><Signature><SignedInfo><Reference><DigestValue>AA==</DigestValue></Reference></SignedInfo></Signature></saml:Assertion></Response>`

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("panic: %v", r)
		}
	}()
	if _, _, err := callback(svc, tiny); err == nil {
		t.Error("forged response accepted")
	}
}

func TestHandleSAMLCallback_ReplayedAssertionIsRefused(t *testing.T) {
	svc, db := newSSOTestService(t)
	saveSAML(t, svc, idpSigner(), true)
	xml := buildSAMLResponse(t, idpSigner(), validSAMLSpec("_replay"))

	if _, _, err := callback(svc, xml); err != nil {
		t.Fatal(err)
	}
	_, token, err := callback(svc, xml)
	if err == nil || !strings.Contains(err.Error(), "replay") || token != "" {
		t.Errorf("second use: token issued = %v, err %v; want a replay refusal", token != "", err)
	}

	testutil.Exec(t, db, `UPDATE saml_used_assertions SET expires_at = NOW() - INTERVAL '1 minute' WHERE assertion_id = '_replay'`)
	if used, err := svc.store.IsAssertionUsed(context.Background(), "_replay"); err != nil || used {
		t.Errorf("expired record still blocks: used = %v, err %v", used, err)
	}
}

func TestHandleSAMLCallback_Preconditions(t *testing.T) {
	ctx := context.Background()
	xml := func(t *testing.T) string { return buildSAMLResponse(t, idpSigner(), validSAMLSpec("_pre")) }

	t.Run("not configured", func(t *testing.T) {
		svc, _ := newSSOTestService(t)
		if _, _, err := callback(svc, xml(t)); err == nil || !strings.Contains(err.Error(), "not configured") {
			t.Errorf("got %v", err)
		}
	})
	t.Run("disabled", func(t *testing.T) {
		svc, _ := newSSOTestService(t)
		saveSAML(t, svc, idpSigner(), false)
		if _, _, err := callback(svc, xml(t)); err == nil || !strings.Contains(err.Error(), "disabled") {
			t.Errorf("got %v", err)
		}
	})
	t.Run("no certificate to verify against", func(t *testing.T) {
		svc, _ := newSSOTestService(t)
		err := svc.SetConfig(ctx, "saml", map[string]string{model.SAMLKeyEntityID: "cz"}, true)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := callback(svc, xml(t)); err == nil || !strings.Contains(err.Error(), "idp_cert") {
			t.Errorf("got %v", err)
		}
	})
	t.Run("login switched off site-wide", func(t *testing.T) {
		svc, _ := newSSOTestService(t)
		saveSAML(t, svc, idpSigner(), true)
		if err := svc.siteSetting.Set(ctx, "allow_login", "false"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := callback(svc, xml(t)); !errors.Is(err, ErrLoginDisabled) {
			t.Errorf("got %v, want ErrLoginDisabled", err)
		}
	})
	t.Run("registration closed", func(t *testing.T) {
		svc, _ := newSSOTestService(t)
		saveSAML(t, svc, idpSigner(), true)
		if err := svc.siteSetting.Set(ctx, "allow_registration", "false"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := callback(svc, xml(t)); !errors.Is(err, ErrRegistrationDisabled) {
			t.Errorf("got %v, want ErrRegistrationDisabled", err)
		}
	})
}

func TestSAMLMetadataXML(t *testing.T) {
	svc, _ := newSSOTestService(t)
	ctx := context.Background()
	if _, err := svc.SAMLMetadataXML(ctx); err == nil {
		t.Error("metadata served with no config")
	}
	saveSAML(t, svc, idpSigner(), false)
	out, err := svc.SAMLMetadataXML(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`entityID="cz"`, `Location="` + samlACS + `"`, `WantAssertionsSigned="true"`} {
		if !strings.Contains(out, want) {
			t.Errorf("metadata lacks %s:\n%s", want, out)
		}
	}
}

func authnRequestXML(t *testing.T, authnURL string) (xml string, query url.Values) {
	t.Helper()
	u, err := url.Parse(authnURL)
	if err != nil {
		t.Fatal(err)
	}
	query = u.Query()
	raw, err := base64.StdEncoding.DecodeString(query.Get("SAMLRequest"))
	if err != nil {
		t.Fatal(err)
	}
	inflated, err := io.ReadAll(flate.NewReader(bytes.NewReader(raw)))
	if err != nil {
		t.Fatal(err)
	}
	return string(inflated), query
}

func TestSAMLAuthnRequestURL(t *testing.T) {
	svc, _ := newSSOTestService(t)
	saveSAML(t, svc, idpSigner(), true)
	ctx := context.Background()

	got, err := svc.SAMLAuthnRequestURL(ctx, "/dashboard?tab=a&b=c", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "https://idp.test.invalid/sso?SAMLRequest=") {
		t.Errorf("url = %s", got)
	}
	xml, q := authnRequestXML(t, got)
	for _, want := range []string{`AssertionConsumerServiceURL="` + samlACS + `"`, `Destination="https://idp.test.invalid/sso"`, `>cz</saml:Issuer>`, `Version="2.0"`} {
		if !strings.Contains(xml, want) {
			t.Errorf("AuthnRequest lacks %s:\n%s", want, xml)
		}
	}
	if strings.Contains(xml, "ForceAuthn") {
		t.Error("ForceAuthn set without being asked")
	}
	if q.Get("RelayState") != "/dashboard?tab=a&b=c" {
		t.Errorf("RelayState = %q", q.Get("RelayState"))
	}

	forced, err := svc.SAMLAuthnRequestURL(ctx, "", true)
	if err != nil {
		t.Fatal(err)
	}
	xml, q = authnRequestXML(t, forced)
	if !strings.Contains(xml, `ForceAuthn="true"`) {
		t.Errorf("ForceAuthn missing:\n%s", xml)
	}
	if _, ok := q["RelayState"]; ok {
		t.Error("empty RelayState was sent")
	}
}

// The HTTP-Redirect binding caps RelayState at 80 bytes; over that it is dropped.
func TestSAMLAuthnRequestURL_RelayStateLimit(t *testing.T) {
	svc, _ := newSSOTestService(t)
	saveSAML(t, svc, idpSigner(), true)
	ctx := context.Background()

	for _, tc := range []struct {
		n    int
		sent bool
	}{{80, true}, {81, false}} {
		got, err := svc.SAMLAuthnRequestURL(ctx, strings.Repeat("a", tc.n), false)
		if err != nil {
			t.Fatal(err)
		}
		if sent := strings.Contains(got, "&RelayState="); sent != tc.sent {
			t.Errorf("%d-byte RelayState: sent = %v, want %v", tc.n, sent, tc.sent)
		}
	}
}

func TestSAMLAuthnRequestURL_Refusals(t *testing.T) {
	ctx := context.Background()

	svc, _ := newSSOTestService(t)
	if _, err := svc.SAMLAuthnRequestURL(ctx, "", false); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Errorf("not configured: got %v", err)
	}
	saveSAML(t, svc, idpSigner(), false)
	if _, err := svc.SAMLAuthnRequestURL(ctx, "", false); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("disabled: got %v", err)
	}
	if err := svc.SetConfig(ctx, "saml", map[string]string{model.SAMLKeyEntityID: "cz"}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SAMLAuthnRequestURL(ctx, "", false); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Errorf("incomplete: got %v", err)
	}
}

func TestExtractSAMLAttribute(t *testing.T) {
	stmts := []samlAttrStmt{
		{Attributes: []samlAttribute{{Name: "Other", Values: []string{"x"}}}},
		{Attributes: []samlAttribute{
			{Name: "MAIL", Values: []string{"  ", " ann@example.test "}},
			{Name: "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name", Values: []string{"ann"}},
			{Name: "empty", Values: []string{" "}},
		}},
	}
	for _, tc := range []struct {
		names []string
		want  string
	}{
		{[]string{"email", "mail"}, "ann@example.test"},
		{[]string{"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name"}, "ann"},
		{[]string{"empty"}, ""},
		{[]string{"absent"}, ""},
	} {
		if got := extractSAMLAttribute(stmts, tc.names...); got != tc.want {
			t.Errorf("extractSAMLAttribute(%v) = %q, want %q", tc.names, got, tc.want)
		}
	}
	if got := extractSAMLAttribute(nil, "email"); got != "" {
		t.Errorf("no statements: got %q", got)
	}
}

func TestParseSAMLTime(t *testing.T) {
	for _, in := range []string{"2026-01-02T03:04:05Z", "2026-01-02T03:04:05.123456Z", "2026-01-02T03:04:05+02:00"} {
		if _, err := parseSAMLTime(in); err != nil {
			t.Errorf("parseSAMLTime(%q): %v", in, err)
		}
	}
	for _, in := range []string{"", "2026-01-02", "03:04:05", "not a time"} {
		if _, err := parseSAMLTime(in); err == nil {
			t.Errorf("parseSAMLTime(%q) accepted", in)
		}
	}
}

func TestParseSAMLCert(t *testing.T) {
	pemText := idpSigner().certPEM
	block, _ := pem.Decode([]byte(pemText))
	rawDER := base64.StdEncoding.EncodeToString(block.Bytes)

	for name, in := range map[string]string{
		"pem":                   pemText,
		"pem with whitespace":   "\n  " + pemText + "\n",
		"bare base64":           rawDER,
		"bare base64 with edge": " " + rawDER + " ",
	} {
		cert, err := parseSAMLCert(in)
		if err != nil || cert.Subject.CommonName != "idp.test.invalid" {
			t.Errorf("%s: cert %v, err %v", name, cert, err)
		}
	}
	for name, in := range map[string]string{
		"empty":             "",
		"blank":             "  \n ",
		"garbage":           "not a certificate",
		"valid pem, no der": "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----",
	} {
		if _, err := parseSAMLCert(in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestExtractXMLElement(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"simple", `<a>x</a>tail`, `<a>x</a>`},
		{"attributes", `<a b="1">x</a>tail`, `<a b="1">x</a>`},
		{"same tag nested", `<a><a>i</a>o</a>tail`, `<a><a>i</a>o</a>`},
		{"self-closing child of the same name", `<a><a/>o</a>tail`, `<a><a/>o</a>`},
		{"prefixed", `<ds:S><ds:SV>v</ds:SV></ds:S>z`, `<ds:S><ds:SV>v</ds:SV></ds:S>`},
	} {
		got, err := extractXMLElement([]byte(tc.in))
		if err != nil || string(got) != tc.want {
			t.Errorf("%s: got %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
	for name, in := range map[string]string{
		"empty":             "",
		"not an element":    "text<a/>",
		"unclosed":          `<a><b></b>`,
		"no tag terminator": `<a`,
	} {
		if got, err := extractXMLElement([]byte(in)); err == nil {
			t.Errorf("%s: got %q, want an error", name, got)
		}
	}
}
