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
