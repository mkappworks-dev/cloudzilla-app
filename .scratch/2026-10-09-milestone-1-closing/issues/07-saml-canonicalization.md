# SAML signature verification without canonicalization

Created: 2026-10-09
Category: enhancement
Status: ready-for-human

## Problem

`verifySAMLSignature` hashes the raw bytes of the assertion and `SignedInfo` instead of applying XML canonicalization (exclusive C14N). Its own doc comment says so and recommends a full SAML library for strict use. A real IdP that signs over canonical bytes (different namespace declaration placement, attribute order, whitespace or self-closing forms) can fail verification, and the code has only been tested against responses signed by the same scheme (see `sso_saml_test.go`).

It also locates elements by string search (`<ds:Signature`, `<Signature`, `<dsig:Signature` and a few `saml:` prefixes), which assumes common prefixes.

## Needs a human

- A decision on adding a dependency such as `github.com/russellhaering/goxmldsig` (the roadmap says "no new Go dependencies" as a rule, so this is a deliberate exception) or implementing exclusive C14N in-house.
- Fixtures from at least one real IdP (Okta, Entra ID or Keycloak) to test against; a Keycloak container is the cheapest.

## Acceptance criteria

- [ ] Verification passes on real signed responses from the chosen IdP, with a fixture committed.
- [ ] Wrapping attacks are refused: a second unsigned assertion, a signature reference to a different element, and a `Reference/@URI` that does not match the assertion ID.
- [ ] Existing SAML tests still pass or are updated to build their fixtures through the new path.

## Blocked by

The dependency decision.

## Comments

**Claude (research session), 2026-10-10:** Recommendation for the dependency decision. Everything below was checked against primary sources or by running it; what I could not verify is marked UNVERIFIED. `Status:` is left at `ready-for-human`.

### Recommendation

**Add `github.com/russellhaering/goxmldsig` at `v1.6.1` or later as a deliberate exception to the "no new Go dependencies" rule, used only to verify the assertion signature. Do not adopt `crewjam/saml`, and do not write exclusive C14N in-house.**

The decision for the human: **approve or reject adding `goxmldsig v1.6.1` (which brings `github.com/beevik/etree v1.8.1` and `github.com/jonboulle/clockwork v0.5.0`, nothing else at runtime) to `go.mod`.** If approved, the follow-up implementation ticket is small (see "What approval unlocks"). If rejected, the in-house route in section 3 is the only alternative, and I would size it at several days rather than one.

The roadmap has already waived the rule once for a bounded purpose (storage backends: aws-sdk-go-v2 and `golang.org/x/image`, under Phase 20 in [ROADMAP.md](../../../docs/ROADMAP.md)), so a second waiver needs a one-line note there as well.

Why: the current verifier is not just imprecise, it rejects a real IdP. I captured a signed response from Keycloak 26.8.0 and ran it through `verifySAMLSignature` unchanged: `saml assertion digest mismatch: signature does not match content`. Keycloak declares `xmlns:saml` on the `Response`, not on the `Assertion`, and prefixes the signature with `dsig:`, so the bytes the IdP hashed (after exclusive C14N) are not the bytes in the document. The same response verifies with `goxmldsig` (section 1), and a tampered `NameID` and an appended unsigned assertion are both refused.

### 1. goxmldsig

| Item | Finding | Source |
| --- | --- | --- |
| Latest release | v1.6.1, 2026-08-04 (v1.6.0 2026-03-18, v1.5.0 2025-03-20) | [pkg.go.dev versions](https://pkg.go.dev/github.com/russellhaering/goxmldsig?tab=versions) |
| License | Apache-2.0 | [repo](https://github.com/russellhaering/goxmldsig) |
| Maintenance | Alive but bursty: the maintainer ships security fixes in batches (Mar and Aug 2026); a gap from 2025-03 to 2026-02 had only dependabot; 24 open PRs, some months old (e.g. [#192](https://github.com/russellhaering/goxmldsig/pull/192) cert chains, [#203](https://github.com/russellhaering/goxmldsig/pull/203)). README says it implements "the subset" of the standards that SAML needs | [commits](https://api.github.com/repos/russellhaering/goxmldsig/commits?per_page=15), [repo](https://github.com/russellhaering/goxmldsig) |
| Runtime deps | `github.com/beevik/etree` (v1.8.1 is current; module needs v1.7.0) and `github.com/jonboulle/clockwork v0.5.0`, both with zero requires of their own. `go.mod` says `go 1.23.0`, Cloudzilla is on 1.27. `testify` and indirects are test-only. Neither etree nor clockwork is in Cloudzilla's `go.mod` today, so the diff is 3 modules | [goxmldsig go.mod](https://raw.githubusercontent.com/russellhaering/goxmldsig/main/go.mod), [etree go.mod](https://raw.githubusercontent.com/beevik/etree/v1.8.1/go.mod), [clockwork go.mod](https://raw.githubusercontent.com/jonboulle/clockwork/v0.5.0/go.mod); confirmed with `go get @latest` + `go mod graph` in a scratch module |
| Exclusive C14N | Yes: `xml-exc-c14n#` with and without comments, `InclusiveNamespaces` PrefixList; also C14N 1.0/1.1. Enveloped-signature transform: yes. Any other transform is an error | [validate.go v1.6.1](https://raw.githubusercontent.com/russellhaering/goxmldsig/v1.6.1/validate.go), [xml_constants.go](https://raw.githubusercontent.com/russellhaering/goxmldsig/v1.6.1/xml_constants.go) |
| Algorithms | RSA and ECDSA with SHA-1/256/384/512; no PSS, no Ed25519. **No allowlist, SHA-1 is accepted** | same, and Go's `x509.CheckSignature` |
| `Reference/@URI` | `Validate(el)` takes the element the caller wants trusted, reads its `ID`, and accepts the first Signature whose Reference URI is `""` or `URI[1:] == ID`. It digests `el` itself, never resolves a URI by searching the document, and returns a **new element re-parsed from the canonical bytes it digested**. `SignedInfo` is also re-read from the signature-verified bytes (the CVE-2020-15216 fix) | [validate.go L270-345, L470-506](https://github.com/russellhaering/goxmldsig/blob/v1.6.1/validate.go) |
| Wrapping attacks | Resistant **if the caller passes the exact element it will consume and reads claims only from the returned element**. Not checked by the library: several matching Signatures (first wins), `URI[1:]` drops any first character (so `xID` matches `ID`), algorithm policy | same |
| Trust model | The cert in `KeyInfo` must equal (`x509.Equal`) a cert in the trusted store, and its validity window is checked against `ctx.Clock`. No chain building. This fits the `idp_cert` pinning Cloudzilla already does | [validate.go L508-564](https://github.com/russellhaering/goxmldsig/blob/v1.6.1/validate.go) |

**Advisories.** I ran `govulncheck v1.8.0` (the version CI pins) against a scratch module that requires v1.6.1: no findings against goxmldsig, etree or clockwork. Known history, all fixed in v1.6.1 or earlier:

- [GHSA-479m-364c-43vc](https://github.com/russellhaering/goxmldsig/security/advisories/GHSA-479m-364c-43vc) / CVE-2026-33487 / [GO-2026-4753](https://pkg.go.dev/vuln/GO-2026-4753): loop-variable capture in `validateSignature`, signature bypass, High 7.5. Fixed in v1.6.0 (affects up to v1.5.0).
- [GHSA-qhrp-hfff-vphr](https://github.com/russellhaering/goxmldsig/security/advisories/GHSA-qhrp-hfff-vphr) (no CVE): quadratic memory use while canonicalizing `SignedInfo`, a ~60 KB request can exhaust memory, High 7.5. Fixed in v1.6.1 (affects up to v1.6.0). There is no `GO-` entry yet, so `govulncheck` would not flag v1.6.0; **pin `>= v1.6.1`**.
- [GHSA-q547-gmf8-8jr7](https://github.com/russellhaering/goxmldsig/security/advisories/GHSA-q547-gmf8-8jr7) / CVE-2020-15216: the original signature-validation bypass, Critical, fixed in v1.1.0.
- [GO-2020-0046](https://pkg.go.dev/vuln/GO-2020-0046) / CVE-2020-7711: nil-pointer panic on a malformed signature (DoS), fixed in v1.1.1. It is not the bypass, despite the common mix-up.
- v1.5.0's note says only "security hardening"; what it changed is UNVERIFIED.

Three of the five are in 2026 and two concern validation or canonicalization logic. That is the real cost of the dependency: expect to bump it when advisories land, and CI's `govulncheck` will surface the ones that get a `GO-` entry.

**Known limits worth knowing.**

- v1.6.1 added a 1000-element traversal budget ([#202](https://github.com/russellhaering/goxmldsig/issues/202): halves the usable document size, no maintainer reply). A normal assertion is far below it (the Keycloak one has 33 elements), but an Entra or Okta assertion with hundreds of group attributes may not be. The limit is not configurable in v1.6.1. UNVERIFIED against a real large assertion.
- From reading the source (not tested): the PrefixList is looked up with key `""` and the spec's `#default` token appears unmapped; only the last C14N transform in a Reference is kept; a code comment calls attribute sorting across namespaces incomplete.
- The only parser-differential defence in the ecosystem is `mattermost/xml-roundtrip-validator`, which `crewjam/saml` and `gosaml2` run on the raw bytes before parsing; `goxmldsig` does not. etree reads with `RawToken` and writes with its own serializer, but whether that makes `goxmldsig` immune to the 2020 `encoding/xml` round-trip class ([Mattermost disclosure](https://mattermost.com/blog/coordinated-disclosure-go-xml-vulnerabilities/), [golang/go#43168](https://github.com/golang/go/issues/43168), declined) is UNVERIFIED. The 2025 "SAMLStorm" family (ruby-saml, node-saml) was not checked at all.

**API fit with `verifySAMLSignature`.** Good, with one flow change. The relevant calls (all in `v1.6.1`):

```go
etreeutils.NSSelectOne(root, "urn:oasis:names:tc:SAML:2.0:assertion", "Assertion") (*etree.Element, error)
ctx := dsig.NewDefaultValidationContext(&dsig.MemoryX509CertificateStore{Roots: []*x509.Certificate{idpCert}})
ctx.Clock = dsig.NewFakeClockAt(t)      // nil Clock = real clock
verified, err := ctx.Validate(el)       // (*etree.Element, error)
```

Two things I hit while running it:

1. Passing a child found with `FindElement` fails with `undeclared namespace prefix: 'saml'`, because the prefix is declared on the `Response`. `etreeutils.NSSelectOne` (or `NSDetatch`) copies the element with the in-scope declarations; this is what `crewjam/saml` does ([service_provider.go L1263-1345](https://github.com/crewjam/saml/blob/main/service_provider.go)).
2. `crewjam/saml` discards the element `Validate` returns and parses the original tree. Cloudzilla should do the opposite: **parse `samlAssertion` only from the verified element** (set it as the root of a fresh `etree.Document`, `WriteToBytes()`, then `xml.Unmarshal`; `etree.Element` has no `WriteToBytes` of its own), so a parser/verifier mismatch cannot matter. Today `HandleSAMLCallback` unmarshals `resp.Assertion` from the raw document, and `encoding/xml` keeps the **last** `<Assertion>` (I checked: with an unsigned one appended, `resp.Assertion` is the unsigned one). That fails closed today only because the unsigned one has no signature.

Wrapping checks, run against the Keycloak response with `goxmldsig`: tampered `NameID` -> `Signature could not be verified`; an appended assertion with no signature -> `Missing signature referencing the top-level element`; clock moved past the cert's `NotAfter` -> `Cert is not valid at this time`. I did not run the "signature reference to a different element" or "URI not matching the ID" cases; both fall under the same `URI[1:] == ID` rule, and the tests in the follow-up ticket should cover them. Cloudzilla should also add its own pre-checks the library skips: exactly one `<Assertion>` child of `Response`, and the Reference URI equals `"#"+ID` (reject `""`).

### 2. Alternatives

| Option | State | Verdict |
| --- | --- | --- |
| **goxmldsig v1.6.1** | Active, Apache-2.0, 2 runtime deps | **Recommended** |
| [crewjam/saml](https://github.com/crewjam/saml) | v0.5.1 (2025-04-14), last commit 2025-05-09, ~17 months stale. BSD-2-Clause. Pins `goxmldsig v1.4.0`, which is exposed to both 2026 advisories, so a consumer must override it. Whole SP/IdP framework plus `jwt`, `x/crypto`, `go-cmp`, `xml-roundtrip-validator` ([go.mod](https://raw.githubusercontent.com/crewjam/saml/main/go.mod)). Advisories 2020-2023, none after ([list](https://github.com/crewjam/saml/security/advisories)) | Reject: heavier, stale, and still needs goxmldsig |
| [russellhaering/gosaml2](https://github.com/russellhaering/gosaml2) | v0.12.0 (2026-08-05), Apache-2.0, needs `go 1.25`, requires goxmldsig v1.6.1. SP only. 9 advisories, 5 in 2026, incl. [GHSA-66r8-42f7-4q74](https://github.com/russellhaering/gosaml2/security/advisories/GHSA-66r8-42f7-4q74) (allocation amplification in the round-trip validator) and unsigned LogoutRequest/Response ([GHSA-pcgw-qcv5-h8ch](https://github.com/russellhaering/gosaml2/security/advisories/GHSA-pcgw-qcv5-h8ch)) | Reject for now: replaces the whole flow (metadata, replay, audience) that Cloudzilla already owns; revisit with Phase 19.1 (encrypted assertions, IdP-initiated) |
| [beevik/etree](https://github.com/beevik/etree) | DOM only, no C14N or signatures | Not an option alone; already part of goxmldsig |
| [ucarion/c14n](https://github.com/ucarion/c14n) | Exclusive C14N only, MIT, dormant since 2020, README admits it ignores processing instructions | Reject: no signature layer, unmaintained |
| [amdonov/xmlsig](https://github.com/amdonov/xmlsig) | Dead since 2018; cannot canonicalize arbitrary XML | Reject |
| [crewjam/go-xmlsec](https://github.com/crewjam/go-xmlsec) | cgo around libxmlsec1, last commit 2020 | Reject: needs cgo and a system libxmlsec1 |

I found no other maintained pure-Go exclusive-C14N library; I only checked the ones above.

### 3. Implementing exclusive C14N in-house

Cost, by my estimate (not from a source): **500-700 lines of new code and 600+ lines of tests, 3-5 days**, against about one day for the goxmldsig route. The library numbers behind it ([v1.6.1](https://github.com/russellhaering/goxmldsig/tree/v1.6.1), `wc -l`): `canonicalize.go` 270, `etreeutils/canonicalize.go` 110, `sort.go` 83, `namespace.go` 448, `unmarshal.go` 43, `validate.go` 584. The exclusive-only path is roughly 350-450 lines on top of etree, and etree does the escaping.

What an in-house version needs, from the specs ([xml-exc-c14n](https://www.w3.org/TR/xml-exc-c14n/), [xml-c14n11](https://www.w3.org/TR/xml-c14n11/), [encoding/xml](https://pkg.go.dev/encoding/xml)):

- `encoding/xml`'s `Token()` replaces prefixes with namespace URLs, so it needs `RawToken()` plus its own namespace stack; there is no safe `Encoder` for this, so the serializer is hand-written (attribute sort by namespace URI then local name, exact escaping with `&#xD;` `&#x9;` `&#xA;` in attributes, empty elements as start/end pairs, CDATA and entities resolved, `xmlns=""` rule, "visibly utilized" prefix rule, `InclusiveNamespaces`).
- Then the validation layer: Reference/URI, enveloped transform, digest, `SignedInfo` canonicalization, signature check, cert pinning (about 580 lines in the library).

Risk: the dependency's own history shows how easy this is to get wrong: four C14N bugs fixed in v1.4.0 ([releases](https://github.com/russellhaering/goxmldsig/releases)), a signature bypass in 2026, a quadratic-memory bug in 2026. An in-house version carries the same bug classes without a community finding them. It would need the W3C and xmlsec test vectors plus fuzzing before I would trust it. It also does not escape the parser-differential question above.

Its only advantage is keeping the "no new dependencies" rule intact, and the project has already waived that rule where a dependency was the safer choice.

### 4. Keycloak fixture plan (checked end to end)

I ran this on Docker with Keycloak [26.8.0](https://github.com/keycloak/keycloak/releases/tag/26.8.0) (released 2026-10-01; [container guide](https://www.keycloak.org/server/containers)). It took about 15 s to boot and imported the realm on first start.

**Where to commit** (mirrors `internal/avatar/testdata` and `internal/attachment/testdata`):

```
internal/service/testdata/saml/keycloak-26.8.0/
  import/cz-saml-test-realm.json   realm import, below
  capture.py                       scripted login, below
  response.xml                     the decoded SAMLResponse
  idp-cert.pem                     the X509Certificate from the response, PEM-wrapped
  README.md                        regeneration steps, Keycloak version, the instant the response is valid at
```

**No secrets.** The realm signing key is generated inside the container on first import and never leaves it; only the public certificate appears in the response (the cert I captured is valid 2026-10-09 to 2036-10-09). The one credential, user `ann` / `fixture-only-pw`, is a throwaway for a container that is destroyed after capture. CI has no secret scanner (`govulncheck` only), but if one is added it will flag the `value` field, so keep that user's password out of any real environment. The container never touches Postgres or the dev DB.

**Run:**

```bash
docker run -d --name cz-kc-saml -p 127.0.0.1:18480:8080 \
  -e KC_BOOTSTRAP_ADMIN_USERNAME=admin -e KC_BOOTSTRAP_ADMIN_PASSWORD=throwaway \
  -v "$PWD/internal/service/testdata/saml/keycloak-26.8.0/import:/opt/keycloak/data/import:ro" \
  quay.io/keycloak/keycloak:26.8.0 start-dev --import-realm
```

Wait for `curl -sf http://127.0.0.1:18480/realms/cz-saml-test/protocol/saml/descriptor`, run `python3 capture.py response.xml`, then `docker rm -f cz-kc-saml`. The realm's public cert is also at that descriptor URL.

<details><summary>import/cz-saml-test-realm.json</summary>

```json
{
  "realm": "cz-saml-test",
  "enabled": true,
  "sslRequired": "none",
  "users": [
    {
      "username": "ann",
      "email": "ann@example.test",
      "firstName": "Ann",
      "lastName": "Test",
      "enabled": true,
      "emailVerified": true,
      "credentials": [{ "type": "password", "value": "fixture-only-pw", "temporary": false }]
    }
  ],
  "clients": [
    {
      "clientId": "https://cz.test.invalid/auth/saml/metadata",
      "protocol": "saml",
      "enabled": true,
      "baseUrl": "https://cz.test.invalid/",
      "redirectUris": ["https://cz.test.invalid/auth/saml/callback"],
      "attributes": {
        "saml.assertion.signature": "true",
        "saml.server.signature": "false",
        "saml.client.signature": "false",
        "saml.signature.algorithm": "RSA_SHA256",
        "saml.canonicalization.method": "http://www.w3.org/2001/10/xml-exc-c14n#",
        "saml_name_id_format": "email",
        "saml.force.post.binding": "true",
        "saml_assertion_consumer_url_post": "https://cz.test.invalid/auth/saml/callback"
      },
      "protocolMappers": [
        {
          "name": "email",
          "protocol": "saml",
          "protocolMapper": "saml-user-property-mapper",
          "config": { "user.attribute": "email", "attribute.name": "email", "attribute.nameformat": "Basic", "friendly.name": "email" }
        },
        {
          "name": "uid",
          "protocol": "saml",
          "protocolMapper": "saml-user-property-mapper",
          "config": { "user.attribute": "username", "attribute.name": "uid", "attribute.nameformat": "Basic", "friendly.name": "uid" }
        }
      ]
    }
  ]
}
```

</details>

The client's `clientId` is the SP entity ID and matches the `entity_id` the test config would use; the redirect and ACS URLs match `acs_url`. I set both signature attributes explicitly (assertion signed, document not) because Cloudzilla only verifies the assertion's signature; I did not check Keycloak's defaults.

<details><summary>capture.py (stdlib only; sends a deflated AuthnRequest, logs in, extracts the POSTed SAMLResponse)</summary>

```python
import base64, html, http.cookiejar, re, sys, urllib.parse, urllib.request, uuid, zlib

KC = "http://127.0.0.1:18480"
REALM = "cz-saml-test"
SP = "https://cz.test.invalid/auth/saml/metadata"
ACS = "https://cz.test.invalid/auth/saml/callback"
out = sys.argv[1]

authn = (
    '<samlp:AuthnRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" '
    'xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" '
    f'ID="_{uuid.uuid4().hex}" Version="2.0" IssueInstant="2026-10-10T00:00:00Z" '
    f'Destination="{KC}/realms/{REALM}/protocol/saml" '
    'ProtocolBinding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" '
    f'AssertionConsumerServiceURL="{ACS}"><saml:Issuer>{SP}</saml:Issuer></samlp:AuthnRequest>'
)
c = zlib.compressobj(9, zlib.DEFLATED, -15)
req = base64.b64encode(c.compress(authn.encode()) + c.flush()).decode()

jar = http.cookiejar.CookieJar()
op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
url = f"{KC}/realms/{REALM}/protocol/saml?" + urllib.parse.urlencode({"SAMLRequest": req, "RelayState": "/"})
page = op.open(url).read().decode()
for ck in jar:
    ck.secure = False  # KC marks cookies Secure even over http; browsers exempt loopback, urllib doesn't
action = html.unescape(re.search(r'<form[^>]*id="kc-form-login"[^>]*action="([^"]+)"', page).group(1))
body = urllib.parse.urlencode({"username": "ann", "password": "fixture-only-pw", "credentialId": ""}).encode()
page = op.open(urllib.request.Request(action, body)).read().decode()
resp = html.unescape(re.search(r'name="SAMLResponse"\s+value="([^"]+)"', page).group(1))
open(out, "wb").write(base64.b64decode(resp))
print("wrote", out)
```

</details>

**What the capture looks like** (facts that shape the tests):

- Keycloak writes `<dsig:Signature xmlns:dsig=...>` inside `<saml:Assertion>`, with `Reference URI="#ID_..."` and two transforms (enveloped-signature, then `xml-exc-c14n#`). `xmlns:saml` is declared only on the `Response`. This is the case the current verifier cannot handle.
- The assertion is valid for **60 seconds** (`Conditions` NotBefore 23:00:45.990Z, NotOnOrAfter 23:01:45.990Z). `HandleSAMLCallback` reads `time.Now()` directly, so a committed fixture can only be tested if the service gets an injectable clock (a `now func() time.Time` field, default `time.Now`). The verifier call also takes the clock for the cert's validity window. Tests set both to the instant in the README.
- Attributes arrive as `email` and `uid`, `NameID` is `ann@example.test` (format emailAddress), `Audience` is the client ID, `Recipient` is the ACS URL. All of these match the existing extraction in `HandleSAMLCallback`.
- Replay protection (`saml_used_assertions`) needs the DB, so the fixture test belongs with the existing SAML tests that use `newSSOTestService`.
- The Secure-cookie gotcha in `capture.py`: Keycloak sets `Secure` cookies even over plain http, and `urllib` will not send them to `127.0.0.1`; a browser would.

Optional second fixture: a response with both the document and the assertion signed (Keycloak's "Sign documents" on) to show the signature layout `Validate` must pick from. It is not needed for the acceptance criteria.

### What approval unlocks

A follow-up ticket (to be written after the decision) would:

1. Add the three modules and note the exception in the ROADMAP.
2. Replace `verifySAMLSignature` (about 138 lines) and `extractXMLElement` (about 45 lines) in `sso_service.go` (now `sso_saml.go` if the split has landed) with a `Validate` call, the two pre-checks above, and parsing from the verified element. Net code goes down.
3. Add the clock to `SSOService`.
4. Commit the fixture and test it; add the three wrapping cases from the acceptance criteria.
5. Rebuild `buildSAMLResponse` in `sso_saml_test.go` on `goxmldsig`'s signing API (`NewDefaultSigningContext` with a `MemoryX509KeyStore`, in `sign.go` and `keystore.go`) so the existing tests produce real exclusive-C14N signatures instead of the raw-byte scheme. This also closes the gap the ticket names: the tests today only prove the verifier agrees with the test helper.
6. Update the "Encrypted assertions..." paragraph in `docs/sso.md` and drop the doc comment that recommends a library.

### Open points that do not block the decision

- Algorithm policy: `goxmldsig` accepts SHA-1. Rejecting `rsa-sha1` and `sha1` digests before `Validate` is cheap and I would include it.
- The 1000-element limit (above) should be tested with a large-group assertion before the first enterprise IdP is onboarded.
- Responses signed only at the `Response` level (Keycloak's "Sign documents" without "Sign assertions") are rejected today and would stay rejected; supporting them is a separate decision.
