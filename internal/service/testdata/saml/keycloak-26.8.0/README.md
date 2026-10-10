# Keycloak 26.8.0 SAML response

A real assertion-signed response from Keycloak 26.8.0 (`quay.io/keycloak/keycloak:26.8.0`), used to test `HandleSAMLCallback` against an IdP that canonicalizes: it declares `xmlns:saml` only on the `Response` and prefixes the signature with `dsig:`.

| File                             | What it is                                                                           |
| -------------------------------- | ------------------------------------------------------------------------------------ |
| `import/cz-saml-test-realm.json` | Realm `cz-saml-test`: client `https://cz.test.invalid/auth/saml/metadata`, user `ann` |
| `capture.py`                     | Logs in as `ann` and writes the decoded `SAMLResponse`                               |
| `response.xml`                   | The captured response                                                                |
| `idp-cert.pem`                   | The `X509Certificate` from that response; the test pins it as `idp_cert`             |

The response is valid at **2026-10-10T14:01:00Z**: `Conditions` run from 14:00:56.615Z to 14:01:56.615Z, and the certificate is valid from 2026-10-10 to 2036-10-10. The test sets the service clock to that instant. `entity_id` is the client ID and `acs_url` is `https://cz.test.invalid/auth/saml/callback`.

No private key is committed: Keycloak generates the realm key inside the container on first import and only the public certificate appears in the response. `ann` / `fixture-only-pw` is a throwaway for a container that is destroyed after the capture.

## Regenerate

Run from the repository root. `capture.py` hard-codes port `18480` in `KC`; if that port is taken, change both together.

```bash
docker run -d --name cz-kc-saml -p 127.0.0.1:18480:8080 \
  -e KC_BOOTSTRAP_ADMIN_USERNAME=admin -e KC_BOOTSTRAP_ADMIN_PASSWORD=throwaway \
  -v "$PWD/internal/service/testdata/saml/keycloak-26.8.0/import:/opt/keycloak/data/import:ro" \
  quay.io/keycloak/keycloak:26.8.0 start-dev --import-realm
```

Wait for `curl -sf http://127.0.0.1:18480/realms/cz-saml-test/protocol/saml/descriptor`, then:

```bash
python3 internal/service/testdata/saml/keycloak-26.8.0/capture.py internal/service/testdata/saml/keycloak-26.8.0/response.xml
docker rm -f cz-kc-saml
```

Copy the `X509Certificate` from the new response into `idp-cert.pem` (PEM, 64-column lines), then set the instant above and `keycloakValidAt` in `sso_saml_keycloak_test.go` to a time inside the new `Conditions` window.
