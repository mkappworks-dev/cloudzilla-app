# Verify SAML assertions with goxmldsig

Created: 2026-10-10
Category: enhancement
Status: ready-for-agent

## Problem

`verifySAMLSignature` hashes raw bytes instead of applying exclusive C14N, so it rejects real IdP responses: a Keycloak 26.8.0 response fails with `saml assertion digest mismatch` because the IdP declares `xmlns:saml` on the `Response` and prefixes the signature with `dsig:`. It also finds elements by string search, checks only `Reference[0]`, ignores `Reference/@URI`, and `HandleSAMLCallback` reads claims from a separately parsed `resp.Assertion` (`encoding/xml` keeps the **last** `<Assertion>`). Ticket 07 has the full evidence.

Decision (Malith Kuruppu, 2026-10-10): add `github.com/russellhaering/goxmldsig` as a deliberate exception to the "no new Go dependencies" rule. The research, advisories and fixture plan are in the Comments of [07](./07-saml-canonicalization.md); read them first.

## Approach

1. **Dependency.** `go get github.com/russellhaering/goxmldsig@v1.6.1` (never below v1.6.1: v1.6.0 has GHSA-qhrp-hfff-vphr, v1.5.0 and below CVE-2026-33487). `go mod tidy` must add only `goxmldsig`, `github.com/beevik/etree` and `github.com/jonboulle/clockwork` to `require`; check with `git diff go.mod`.
2. **Verifier.** Replace `verifySAMLSignature` and `extractXMLElement` with one function that takes the response bytes, the pinned cert and the current time and returns the **verified assertion**:
   - Parse with `etree`. Pre-checks on the unverified tree (policy, not the security boundary): exactly one `Assertion` in the response, and it is a direct child of `Response`; its `Signature` has a `Reference` whose `URI` is exactly `"#"+ID` (reject `""` and any other value, which `goxmldsig` accepts); `SignatureMethod` and `DigestMethod` are not SHA-1.
   - Select it with `etreeutils.NSSelectOne(root, "urn:oasis:names:tc:SAML:2.0:assertion", "Assertion")` so ancestor namespace declarations come along (a plain `FindElement` fails with `undeclared namespace prefix`).
   - `dsig.NewDefaultValidationContext(&dsig.MemoryX509CertificateStore{Roots: []*x509.Certificate{cert}})`, set `Clock` from the service clock, call `Validate`.
   - Serialize the **returned** element (root of a fresh `etree.Document`, `WriteToBytes`) and `xml.Unmarshal` it into `samlAssertion`. Do not read the assertion from the original tree again.
3. **`HandleSAMLCallback`.** Use only the verified assertion for the ID, conditions, subject, audience, recipient and attributes. Keep the `Response` status check; drop `resp.Assertion` and the now unused `dsSignature` types.
4. **Clock.** Add `now func() time.Time` to `SSOService` (default `time.Now`, `NewSSOService` signature unchanged) and use it for the `NotBefore`/`NotOnOrAfter` checks and the validation context, so the Keycloak fixture (valid for 60 s) can be tested.
5. **Docs.** `docs/sso.md` SAML section: signature checked with exclusive C14N and enveloped-signature, `Reference` must be `#<assertion ID>`, exactly one assertion, SHA-1 refused, Response-level-only signatures still not accepted. `docs/ROADMAP.md`: waive the "no new Go dependencies" rule for SAML signature verification next to the existing storage waiver (Phase 20). Drop the doc comment that recommends a full SAML library.

The code is in `internal/service/sso_service.go`, or `sso_saml.go` once ticket 04 lands; find it with `grep -rn verifySAMLSignature internal`.

## Acceptance criteria

- [ ] `go.mod` gains `goxmldsig` (>= v1.6.1), `etree` and `clockwork`, nothing else; `make lint`, `go test ./...` and `go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...` pass.
- [ ] The Keycloak 26.8.0 fixture is committed under `internal/service/testdata/saml/keycloak-26.8.0/` as `import/cz-saml-test-realm.json`, `capture.py`, `response.xml`, `idp-cert.pem` and a `README.md` (regeneration steps, Keycloak version, the instant the response is valid at), with no private key. The realm JSON and script are in the 07 Comments; regenerate `response.xml` and `idp-cert.pem` by running them, and check the committed response against the 07 description (`dsig:` prefix, `xmlns:saml` only on `Response`, 60 s `Conditions`).
- [ ] A test signs in with that real response (clock set to the instant in the README, `entity_id` = the client ID, `acs_url` = the callback URL) and provisions the user.
- [ ] Wrapping and tampering are refused, each with its own test: a signed assertion plus an appended unsigned one; an unsigned one placed before the signed one; a signature whose `Reference` points at a different element (the `Response` or another assertion); `Reference/@URI` of `""`; a `URI` that does not match the assertion ID; a tampered `NameID` after signing; a signature from another key; a SHA-1 signature or digest.
- [ ] `buildSAMLResponse` in `sso_saml_test.go` signs with `goxmldsig` (`NewDefaultSigningContext` with a `MemoryX509KeyStore`, `MakeC14N10ExclusiveCanonicalizerWithPrefixList("")`, `SignEnveloped`) and declares `xmlns:saml` on the `Response`, so the existing tests exercise real exclusive-C14N signatures. The existing assertions in those tests still pass.
- [ ] Mutation check: a test for each wrapping case fails when the pre-checks are removed or when claims are read from the unverified tree (run it, note the result in a comment).
- [ ] Size check: sign an assertion with 300 attributes and record in a comment whether it verifies (goxmldsig v1.6.1 has a fixed 1000-element traversal budget, issue #202). If a realistic size fails, raise it with the human; do not work around it.
- [ ] Docs updated as in Approach 5.

## Out of scope

Response-level-only signatures, encrypted assertions, IdP-initiated login and configurable NameID (Phase 19.1), the `xml-roundtrip-validator` pre-check, and a second fixture with both document and assertion signed.

## Blocked by

Nothing. If ticket 04 has not landed, edit `sso_service.go`; if it has, `sso_saml.go`.
