package service

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
	"github.com/russellhaering/goxmldsig/etreeutils"
)

const (
	dsigSHA1Digest   = "http://www.w3.org/2000/09/xmldsig#sha1"
	dsigSHA256Digest = "http://www.w3.org/2001/04/xmlenc#sha256"
	dsigRSASHA256    = "http://www.w3.org/2001/04/xmldsig-more#rsa-sha256"
)

// handSignedAssertion signs a by hand, where goxmldsig's signing context ties the
// digest to the signature hash and always writes "#"+ID: it lets a test pick the
// Reference URI and the digest algorithm independently of the RSA-SHA256 signature.
func handSignedAssertion(t *testing.T, signer *samlSigner, a *etree.Element, uri, digestAlg string, digestHash crypto.Hash) *etree.Element {
	t.Helper()
	scratch := samlResponseRoot(samlSuccess)
	scratch.AddChild(a)
	self, err := etreeutils.NSSelectOne(scratch, samlNS, "Assertion")
	if err != nil || self == nil {
		t.Fatalf("detach assertion: %v", err)
	}
	canon := dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
	body, err := canon.Canonicalize(self)
	if err != nil {
		t.Fatal(err)
	}
	digest := digestHash.New()
	digest.Write(body)

	signedInfo := etree.NewDocument()
	err = signedInfo.ReadFromString(fmt.Sprintf(`<ds:SignedInfo xmlns:ds="%[1]s"><ds:CanonicalizationMethod Algorithm="%[2]s"/><ds:SignatureMethod Algorithm="%[3]s"/><ds:Reference URI="%[4]s"><ds:Transforms><ds:Transform Algorithm="%[5]s"/><ds:Transform Algorithm="%[2]s"/></ds:Transforms><ds:DigestMethod Algorithm="%[6]s"/><ds:DigestValue>%[7]s</ds:DigestValue></ds:Reference></ds:SignedInfo>`,
		dsig.Namespace, canon.Algorithm(), dsigRSASHA256, uri, dsig.EnvelopedSignatureAltorithmId, digestAlg, base64.StdEncoding.EncodeToString(digest.Sum(nil))))
	if err != nil {
		t.Fatal(err)
	}
	canonSignedInfo, err := canon.Canonicalize(signedInfo.Root())
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canonSignedInfo)
	raw, err := rsa.SignPKCS1v15(rand.Reader, signer.key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}

	sig := etree.NewElement("ds:Signature")
	sig.CreateAttr("xmlns:ds", dsig.Namespace)
	sig.AddChild(signedInfo.Root())
	sig.CreateElement("ds:SignatureValue").SetText(base64.StdEncoding.EncodeToString(raw))
	sig.CreateElement("ds:KeyInfo").CreateElement("ds:X509Data").CreateElement("ds:X509Certificate").SetText(base64.StdEncoding.EncodeToString(signer.certDER))
	self.AddChild(sig)
	self.Attr = slices.DeleteFunc(self.Attr, func(at etree.Attr) bool { return at.Space == "xmlns" })
	return self
}

// The baseline keeps the cases below honest: they differ from it only in the field they name.
func TestHandleSAMLCallback_AcceptsAHandSignedAssertion(t *testing.T) {
	svc, _ := newSSOTestService(t)
	saveSAML(t, svc, idpSigner(), true)
	a := handSignedAssertion(t, idpSigner(), samlAssertionElement(t, validSAMLSpec("_hand")), "#_hand", dsigSHA256Digest, crypto.SHA256)

	if _, _, err := callback(svc, samlResponseOf(t, a)); err != nil {
		t.Fatal(err)
	}
}

// The evil assertion names admin@idp.test and the signed one ann@idp.test, so either
// leaking through shows up as a provisioned SSO user.
//
// Mutation check, run against this test:
//   - without verifySAMLAssertion's pre-checks the appended, extension-wrapped, nested,
//     empty-URI, missing-# and both SHA-1 cases sign in as ann, because goxmldsig accepts them;
//   - the placed-before, copied-signature, other-ID-URI and two-signature cases still fail
//     to verify, so there the pre-checks only decide which error is reported;
//   - reading the claims from the raw response instead of the verified element, on top of
//     that, flips no further case: the one-assertion pre-check is what makes the two agree,
//     so no test here isolates the claim source.
func TestHandleSAMLCallback_RefusesWrappedAndForgedAssertions(t *testing.T) {
	svc, db := newSSOTestService(t)
	saveSAML(t, svc, idpSigner(), true)

	signed := func(id string, tweak func(*dsig.SigningContext)) *etree.Element {
		return signSAMLAssertion(t, idpSigner(), samlAssertionElement(t, validSAMLSpec(id)), tweak)
	}
	evil := func(id string) *etree.Element {
		s := validSAMLSpec(id)
		s.NameID = "admin@idp.test"
		s.Attrs = map[string]string{"email": "admin@example.test", "uid": "admin"}
		return samlAssertionElement(t, s)
	}
	signatureOf := func(el *etree.Element) *etree.Element { return el.FindElement("./Signature").Copy() }
	// responseSignature is what an IdP that signs the Response (and not the assertion) emits.
	responseSignature := func() *etree.Element {
		return signatureOf(signSAMLElement(t, idpSigner(), samlResponseRoot(samlSuccess), nil))
	}
	sha1 := func(c *dsig.SigningContext) { c.Hash = crypto.SHA1 }

	for _, tc := range []struct {
		name    string
		build   func() string
		wantErr string
	}{
		{"unsigned assertion appended after the signed one", func() string {
			return samlResponseOf(t, signed("_w1", nil), evil("_w1x"))
		}, "exactly one assertion"},
		{"unsigned assertion placed before the signed one", func() string {
			return samlResponseOf(t, evil("_w2x"), signed("_w2", nil))
		}, "exactly one assertion"},
		{"signed assertion inside an extension element", func() string {
			ext := etree.NewElement("samlp:Extensions")
			ext.AddChild(signed("_w3", nil))
			return samlResponseOf(t, ext)
		}, "direct child"},
		{"unsigned assertion nested in the signed one", func() string {
			a := samlAssertionElement(t, validSAMLSpec("_w4"))
			a.FindElement("./Subject").AddChild(evil("_w4x"))
			return samlResponseOf(t, signSAMLAssertion(t, idpSigner(), a, nil))
		}, "exactly one assertion"},
		{"signature copied from the Response", func() string {
			a := evil("_w5")
			a.AddChild(responseSignature())
			return samlResponseOf(t, a)
		}, "reference"},
		{"signature copied from another assertion", func() string {
			a := evil("_w6")
			a.AddChild(signatureOf(signed("_w6other", nil)))
			return samlResponseOf(t, a)
		}, "reference"},
		{"Reference URI empty", func() string {
			return samlResponseOf(t, signed("_w7", func(c *dsig.SigningContext) { c.IdAttribute = "NoSuchID" }))
		}, "reference"},
		{"Reference URI names another ID", func() string {
			a := samlAssertionElement(t, validSAMLSpec("_w8"))
			a.CreateAttr("Alt", "_w8other")
			return samlResponseOf(t, signSAMLAssertion(t, idpSigner(), a, func(c *dsig.SigningContext) { c.IdAttribute = "Alt" }))
		}, "reference"},
		{"two signatures", func() string {
			a := signed("_w9", nil)
			a.AddChild(responseSignature())
			return samlResponseOf(t, a)
		}, "exactly one signature"},
		{"NameID changed after signing", func() string {
			return strings.Replace(samlResponseOf(t, signed("_w10", nil)), "ann@idp.test", "admin@idp.test", 1)
		}, "could not be verified"},
		{"signed by another key", func() string {
			return samlResponseOf(t, signSAMLAssertion(t, otherSigner(), samlAssertionElement(t, validSAMLSpec("_w11")), nil))
		}, "not verify certificate"},
		{"SHA-1 signature and digest", func() string {
			return samlResponseOf(t, signed("_w12", sha1))
		}, "SHA-1"},
		{"SHA-1 digest under an SHA-256 signature", func() string {
			a := handSignedAssertion(t, idpSigner(), samlAssertionElement(t, validSAMLSpec("_w13")), "#_w13", dsigSHA1Digest, crypto.SHA1)
			return samlResponseOf(t, a)
		}, "SHA-1"},
		{"Reference URI missing its #", func() string {
			// goxmldsig compares URI[1:] with the ID, so any first character passes it.
			a := handSignedAssertion(t, idpSigner(), samlAssertionElement(t, validSAMLSpec("_w14")), "__w14", dsigSHA256Digest, crypto.SHA256)
			return samlResponseOf(t, a)
		}, "reference"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, token, err := callback(svc, tc.build())
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
		t.Errorf("refused responses provisioned %d saml users (err %v)", n, err)
	}
}
