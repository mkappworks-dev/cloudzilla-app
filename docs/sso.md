# SSO (LDAP and SAML)

`SSOService` (`internal/service/sso_service.go`, with `sso_ldap.go`, `sso_saml.go` and `sso_provision.go` beside it) signs users in through a corporate directory (LDAP) or an identity provider (SAML 2.0). A superadmin configures each provider at `/admin/sso`; the settings live in `sso_configs` (migration 106), one row per provider, with the provider's settings as JSONB. Sign-in links an account through `users.sso_provider` and `users.sso_id`, which are unique together. For how the sessions, 2FA and confirmation prompts treat SSO accounts, see [access-control](./access-control.md).

## Settings

A provider turns on only when the settings sign-in needs are saved (`model.SSOSettingsReady`), and a save cannot clear one of them while the provider is on (`ErrSSOIncomplete`). Saving leaves the provider on or off; the switch on its card (`POST /api/admin/sso/{provider}/enabled`) changes that alone. Saving and switching both ask the superadmin to confirm with their password (and 2FA code), because whoever controls the directory or IdP can sign in as its users.

| Provider | Required                                         | Optional                                  |
| -------- | ------------------------------------------------ | ----------------------------------------- |
| LDAP     | `host`, `bind_dn_tmpl`                           | `port` (389), `base_dn`, `use_tls`        |
| SAML     | `entity_id`, `sso_url`, `acs_url`, `idp_cert`    | —                                         |

`bind_dn_tmpl` holds one `%s`, for example `uid=%s,ou=people,dc=example,dc=com`. `idp_cert` is the IdP's signing certificate, base64 with no PEM header. `acs_url` is Cloudzilla's own `/auth/saml/callback` URL as the IdP will reach it.

## LDAP

`POST /auth/ldap` (`username`, `password`) calls `AuthenticateLDAP`:

1. Escapes the username per RFC 4514 and interpolates it into `bind_dn_tmpl`.
2. Performs a simple bind as that DN. There is no search step: the template alone decides which DN signs in.
3. Uses the bind DN as the account's `sso_id`.

The directory returns no email, so a new account gets `<username>@ldap.local`.

## SAML

SP-initiated only. `GET /auth/saml` redirects to `sso_url` with a deflated `AuthnRequest` (HTTP-Redirect binding) and the `next` path as `RelayState`, which is dropped above the binding's 80-byte limit. The IdP posts to `POST /auth/saml/callback`, and `HandleSAMLCallback` accepts the response only if:

- the status is success and the assertion's signature verifies against `idp_cert` (see Signature below);
- `NotBefore` and `NotOnOrAfter` hold (an assertion with no expiry is valid for 5 minutes);
- its ID has not been seen before (`saml_used_assertions`, migration 037), so a captured response cannot be replayed;
- an `AudienceRestriction` names `entity_id` (a missing one fails too), and `Recipient`, when present, equals `acs_url`;
- it carries a `NameID`, which becomes the `sso_id`.

The username comes from the first of `uid`, `username`, `sAMAccountName` or the `…/claims/name` attribute, falling back to the `NameID`. The email comes from `email`, `mail`, `emailAddress` or the `…/claims/emailaddress` attribute, falling back to the `NameID`. `GET /auth/saml/metadata` serves the SP metadata (ACS location, `WantAssertionsSigned="true"`). Encrypted assertions, IdP-initiated login and LDAP group mapping are not supported; they are planned as Phase 19.1.

### Signature

`verifySAMLAssertion` checks the signature with [goxmldsig](https://github.com/russellhaering/goxmldsig) (exclusive C14N and the enveloped-signature transform), so a response from a real IdP such as Keycloak verifies whatever prefixes it uses or where it declares them. Only a signature on the assertion counts: a signed `Response` around an unsigned assertion is refused. Before the library runs, Cloudzilla refuses a response that:

- has anything but exactly one `Assertion`, which must be a direct child of `Response`;
- carries more than one `Signature` on the assertion, or a `Signature` with more than one `Reference`;
- has a `Reference/@URI` that is not exactly `#<assertion ID>` (the library also accepts `""`);
- uses SHA-1 for the signature or the digest (the library accepts it).

Claims are read from the element the library returns, never from the response as received. The certificate in the signature must be `idp_cert` itself and valid at the time of the check; there is no chain building. The library stops after 1000 XML elements, so an assertion with about 500 attributes or more is refused ("traversal limit reached"); 480 verify.

## Accounts

`findOrProvisionUser` runs for both providers:

1. An account already linked to the `(provider, sso_id)` pair signs in.
2. Otherwise an account that owns the email is **not** linked: the sign-in fails and the user must sign in another way. Only the IdP operator vouches for the address, so it never counts as proof of ownership.
3. Otherwise a new account is created (`ProvisionSSOUser`), with no password, if `allow_registration` is on; if it is off, sign-in fails with `ErrRegistrationDisabled`. The username is the asserted one lowercased, with other characters replaced by `_` and fitted to the owner-name rule.

Both providers refuse when `allow_login` is off (`ErrLoginDisabled`), and a suspended account gets `ErrAccountSuspended`. A user with 2FA on is sent to `/auth/2fa` before the session starts.

## Routes

| Method   | Path                                | Notes                                                           |
| -------- | ----------------------------------- | --------------------------------------------------------------- |
| GET/POST | `/admin/sso`                        | Settings page and save; superadmin                              |
| POST     | `/api/admin/sso/{provider}/enabled` | `enabled=true\|false`; 422 until the required settings are saved |
| POST     | `/auth/ldap`                        | `username`, `password`; rate-limited per IP                      |
| GET      | `/auth/saml`                        | Redirect to the IdP                                             |
| POST     | `/auth/saml/callback`               | Assertion consumer service                                      |
| GET      | `/auth/saml/metadata`               | SP metadata XML                                                 |
