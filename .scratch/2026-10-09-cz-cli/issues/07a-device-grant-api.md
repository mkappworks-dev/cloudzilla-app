# Device grants: table, service and token endpoints

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent
Blocked by: 02

Part of [07](./07-device-code-login.md); read its Design section first. This ticket is the server's grant lifecycle and the two JSON endpoints. The browser approval page is [07b](./07b-device-approval-page.md); `cz` is [07c](./07c-cz-device-login.md).

## What to build

- Migration `device_grants` (next free number), model, `DeviceGrantStore` wired into `Stores`, `DeviceGrantService` wired into `Services`. Layering as in `CLAUDE.md`.
- Service methods: create a grant (validates scopes, rejects `repo:admin`, caps `device_name`), look up by user code, approve (with the chosen scopes, narrowed from the requested ones), deny, and redeem on poll (`approved → consumed` and `AccessTokenService.Create` in one transaction, token named `cz (<hostname>) · <date>`, no expiry). The 07b handlers call approve and deny; this ticket tests them at service level.
- `POST /api/auth/device/code` and `POST /api/auth/device/token` as specified, with `Cache-Control: no-store` and RFC 8628 error bodies.
- CSRF exemption for both paths in `internal/middleware/csrf.go`. Do not add them to `internal/middleware/setup.go` (see 07).
- Limits: 20 code requests per hour per IP, at most 5 live grants per IP, interval enforcement from `last_polled_at` with a persisted `slow_down` (+5 s), and a per-IP request limit on the token endpoint.
- Lazy cleanup of grants older than a day on code request.
- `docs/api-reference.md`: both endpoints, the error table, the limits.

## Acceptance criteria

- [ ] A code request returns the documented fields; an unknown scope or `repo:admin` gets `400 invalid_scope`; the default scope is `repo:write`.
- [ ] Polling returns `authorization_pending` until approval, then the token once; a second poll gets `invalid_grant`.
- [ ] A poll inside `interval` gets `slow_down` and the interval grows by 5 s.
- [ ] Denied, expired (900 s) and consumed grants answer `access_denied`, `expired_token`, `invalid_grant`; none can be revived.
- [ ] Two concurrent polls of one approved grant mint exactly one token.
- [ ] The minted token is `czp_`, hash-only, carries only the approved scopes, has the documented name, and lists in Settings → Tokens; revoking it works.
- [ ] Both endpoints work without a CSRF token or cookie; the browser forms from 07b do not gain that exemption.
- [ ] Per-IP code-request and live-grant limits return `429` with `Retry-After`.
- [ ] Tests cover the lifecycle and each error response; `docs/api-reference.md` is updated.

## Comments
