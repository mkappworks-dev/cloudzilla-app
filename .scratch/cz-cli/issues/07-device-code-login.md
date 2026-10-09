# Device-code login for cz

Created: 2026-10-09
Category: enhancement
Status: needs-triage
Blocked by: 02

Part of [spec](../spec.md). Stage 2; needs a design pass and security review before it is `ready-for-agent`.

## What to build

A device-code flow modelled on GitHub's OAuth device flow, so `cz auth login` needs no pasted token and works for users with 2FA.

- `POST /api/auth/device/code` → `device_code`, `user_code`, `verification_uri`, `expires_in` (900), `interval`.
- `/login/device`: signed-in user enters the user code, sees what is being authorised (client name, scopes), and approves with their password and, with 2FA, a code (as `POST /oauth/authorize` does).
- `POST /api/auth/device/token`: poll; returns `authorization_pending`, `slow_down` (add 5 s), `expired_token`, `access_denied`, or the token.
- The token is a PAT: same `czp_` format, hash-only storage, scopes chosen at request time and capped to non-`repo:admin` scopes.
- A migration for pending grants; a one-time `user_code`; rate limits on code entry (GitHub allows 50 per hour per app) and on polling.
- `cz auth login` uses it by default; `--with-token` stays.

Open questions to settle in the design pass: reuse the OAuth-app tables with a built-in `cz` client, or a separate table; how the token is named in Settings → Tokens so a user can revoke it; whether `repo:admin` is ever grantable this way (the spec says no).

## Acceptance criteria

- [ ] Login completes end to end for a user with 2FA on.
- [ ] Codes expire after 15 minutes and can't be redeemed twice; a denied code is dead.
- [ ] Polling faster than `interval` gets `slow_down`; code entry is rate limited.
- [ ] The issued token appears in Settings → Tokens and can be revoked there.
- [ ] `docs/api-reference.md` and `docs/access-control.md` document the endpoints and the threat model.
- [ ] Tests cover the grant lifecycle and each error response.

## Comments
