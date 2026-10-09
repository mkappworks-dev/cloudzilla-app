# Device-code login for cz

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent
Blocked by: 02

Part of [spec](../spec.md). Stage 2. Design approved by the maintainer on 2026-10-09; it is built by [07a](./07a-device-grant-api.md), [07b](./07b-device-approval-page.md) and [07c](./07c-cz-device-login.md). Set this ticket `done` when all three are.

## What to build

A device-code flow modelled on GitHub's OAuth device flow (RFC 8628), so `cz auth login` needs no pasted token and works for users with 2FA. GitHub's `gh` runs the same flow as a registered OAuth App; `cz` is first-party, so it is a built-in client with no registration and no secret.

## Design

### Decisions

1. **New `device_grants` table; approval mints a normal PAT.** Reusing `oauth_apps`/`oauth_authorizations` was rejected: `UNIQUE(app_id, user_id)` plus the `Upsert` that overwrites `token_hash` would make a second machine's login revoke the first, and those tokens are OAuth-app tokens, not `czp_` PATs in `access_tokens`. Third-party apps keep `/oauth/authorize`; a later `client_id` column on `device_grants` would admit third-party device clients.
2. **Token name: `cz (<hostname>) · <date>`.** `cz` sends the hostname as `device_name`; the server strips control characters, caps it at 40 characters, falls back to `cz CLI`, and the approval page shows it as unverified.
3. **Scopes.** `cz auth login` requests `repo:write` by default (the only scope that admits merge, repo create/fork and push); `--scope` narrows. The approval page shows the requested scopes as pre-ticked boxes the user may untick, never add to, with at least one left. `repo:admin` and unknown scopes are rejected at code request (`400 invalid_scope`) and again at approval.
4. **Approval re-checks `Reauth.Confirm`**, as `POST /oauth/authorize` does: password plus a code with 2FA; LDAP users type their directory password; Google/SAML users use "Confirm with Google/SSO" (`cz_reauth` cookie) or an emailed code, via `components.ConfirmFactors`. Shares the 5-failures-per-15-minutes per-user throttle. Denying needs nothing.
5. **No expiry on the issued token**, as for a manually created non-admin PAT; `cz` has no refresh logic. Mitigations: an email on approval, `last_used_at` in Settings, one-click revoke.
6. **The code never travels in a URL.** No `verification_uri_complete`, no `?user_code=`: a one-click link would let a phisher skip the step where the user compares the code with their terminal, and URLs leak into history and logs. `cz` copies the code to the clipboard instead, as `gh` does.
7. **Token shown once, at the first successful poll**, not at approval, so the raw token never sits in the database.

### Table: `device_grants` (migration: next free number; 111 on `feat/cz-cli`)

`id`, `device_code_hash` (SHA-256, unique), `user_code` (normalized, no hyphen, unique among live rows), `scopes`, `device_name`, `status` (`pending|approved|denied|consumed`), `user_id` (null until approved or denied), `requester_ip`, `interval_secs` (starts 5), `last_polled_at`, `expires_at` (created + 900 s), `created_at`. Rows older than a day are deleted lazily on code request.

### Endpoints

- `POST /api/auth/device/code`: form or JSON `scope` (space-separated, default `repo:write`), `device_name`. Answers `{device_code, user_code, verification_uri, expires_in: 900, interval: 5}`. `user_code` is `XXXX-XXXX`; `verification_uri` is `<host>/login/device`. There is no `verification_uri_complete` (RFC 8628 makes it optional): a link that carries the code would take a phished user straight to the confirm step.
- `POST /api/auth/device/token`: `grant_type=urn:ietf:params:oauth:grant-type:device_code`, `device_code`. Success `200 {access_token, token_type: "bearer", scope}`. Errors are `400 {"error": …}`: `authorization_pending`, `slow_down` (interval +5 s, persisted), `expired_token`, `access_denied`, `invalid_grant` (unknown or already consumed), `unsupported_grant_type`, `invalid_request`. Every response carries `Cache-Control: no-store`.
- Both are added to the CSRF exemption beside `/oauth/token` in `internal/middleware/csrf.go`: they carry no cookie. The browser forms keep CSRF.
- Both are outside `middleware/scope.go`'s allow-list on purpose: they authenticate by device code, not by a token. They are not added to `middleware/setup.go`'s path check: no user exists before setup completes, so nothing can be approved.
- `GET /login/device` (`optAuthMW`): code entry; a signed-out user is redirected to `/login?next=…`. `POST /login/device` (entry, 50 per hour per user) renders the confirm step: device name (marked unverified), requester IP and time, scopes. `POST /login/device/approve` (`action=approve|deny`, `scope[]`, `password`, `code`). No route accepts the code in a query string; the user always types it. The confirm page sends `X-Frame-Options: DENY` and `frame-ancestors 'none'`.

### Lifecycle (all transitions are conditional `UPDATE`s, so races lose)

`pending → approved | denied` at the browser; `approved → consumed` at the first poll, in one transaction that inserts the `access_tokens` row (`czp_`, hash-only) and returns the raw token once. Denied, expired and consumed grants are dead and answer `access_denied`/`expired_token`/`invalid_grant`.

### Entropy and rate limits

- `device_code`: 32 random bytes, hex; only the SHA-256 is stored.
- `user_code`: 8 characters from `BCDFGHJKLMNPQRSTVWXZ` (~34 bits). Sign-in is also required to guess, so the exposure is a few live codes per signed-in attacker at 50 tries per hour.
- Code request: 20 per hour per IP (`middleware.RateLimit`); at most 5 live grants per IP.
- Code entry: 50 per hour per user, in process like the other auth limits (each instance of a multi-instance deployment counts alone).
- Polling: the interval is enforced from `last_polled_at` in the database; an early poll gets `slow_down` and the interval grows by 5 s. A per-IP request limit backs it up.
- Wrong passwords or codes: the shared reauth throttle.

### Audit and notice

New events: `user.device.approve`, `user.device.deny`, and `user.token.create` for the minted token (recorded with the source "device login"; PAT creation has no audit event today, and this adds one for this path only). Approval mails the account a notice with the device name, IP and scopes.

### `cz auth login`

Device flow is the default. It prints the URL and code to stderr always, copies the code to the clipboard where it can, and opens the plain URL in a browser only on a TTY without `--no-browser`. It needs no stdin, so it works headless and over SSH with approval on another machine. It honors `slow_down`, stops at `expires_in`, cancels on Ctrl-C, and verifies the token with `GET /api/user` before saving, as the PAT path does. `--with-token` and `CZ_TOKEN` are unchanged. A `404` from the code endpoint (an older server) prints a pointer to `--with-token`. `--scope` narrows the request.

### Threat model

| Threat | Mitigation |
| --- | --- |
| Phishing: an attacker starts a flow and sends the victim the code | Confirm step shows device name, IP and scopes; the password is re-asked; the owner is mailed; the token is revocable and `last_used_at` visible. A residual risk remains, as with GitHub's flow, and `docs/access-control.md` must say so. |
| Guessing a user code | Needs sign-in; 50 entries per hour per user; ~34 bits against a few live codes. |
| Polling brute force or a leaked `device_code` | 256-bit value, hash-only storage, interval enforcement, token returned once. A leaked code is useless until a user approves, and an approved grant is consumed by the first poll. |
| Replay or double redemption | Conditional `UPDATE`s; dead states are terminal. |
| Scope escalation to `repo:admin` | Rejected at code request and at approval. |
| A link that carries the code (phishing, history, logs) | None exists; the user types the code after reading it from their own terminal. |
| Spoofed device name | Displayed as unverified, capped, stripped, HTML-escaped by Templ. |

### Split

| Ticket | Scope | Blocked by |
| --- | --- | --- |
| [07a](./07a-device-grant-api.md) | Migration, model, store, service, the two JSON endpoints, CSRF exemption, limits, lifecycle tests, `api-reference.md` | 02 |
| [07b](./07b-device-approval-page.md) | `/login/device` pages, reauth, scope narrowing, email notice, audit events, `access-control.md` and threat model | 07a |
| [07c](./07c-cz-device-login.md) | `cz auth login` device flow, fallbacks, `docs/cli.md` | 07a |

## Acceptance criteria

- [ ] Login completes end to end for a user with 2FA on (07b + 07c).
- [ ] Codes expire after 15 minutes and can't be redeemed twice; a denied code is dead (07a).
- [ ] Polling faster than `interval` gets `slow_down`; code entry is rate limited (07a, 07b).
- [ ] The issued token appears in Settings → Tokens and can be revoked there (07a).
- [ ] `docs/api-reference.md` and `docs/access-control.md` document the endpoints and the threat model (07a, 07b).
- [ ] Tests cover the grant lifecycle and each error response (07a).

## Comments

2026-10-09, design pass with the maintainer: decisions above agreed one at a time. Open for implementation: none blocking. Re-check the migration number against `origin/main` when 07a starts.
