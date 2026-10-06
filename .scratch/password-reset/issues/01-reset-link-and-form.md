# Reset a password from a link

Created: 2026-10-06
Category: enhancement
Status: done

Spec: [../spec.md](../spec.md)

## What to build

The core of the reset, end to end. It works before any email is sent, because tests (and later the CLI) get a link from `IssueLink`.

- A migration for `password_reset_tokens` (the next free number at commit time) with the columns, uniqueness and cascade described under "Tokens" in the spec.
- `PasswordResetStore`, wired into `Stores`. It covers issuing (an upsert, one row per user), looking up by hash, and spending: one transaction that locks the user row first, checks every usability condition, and then sets the hash, bumps `session_version`, sets `email_verified_at` when it is NULL, and marks the row used.
- The token helpers move out of the email-verification service into shared, neutrally named helpers, and email verification uses them too.
- `PasswordResetService`, wired into `Services`, with `IssueLink(ctx, userID, issuedBy)`, `Check(ctx, token)` and `Reset(ctx, token, newPassword, code)`. `IssueLink` refuses an account without a password and returns a 24-hour link.
- `GET`/`POST /auth/password/reset/{token}`: the page names the account and has new password, confirm, and (for 2FA accounts) code fields. It sends `Referrer-Policy: no-referrer` and `Cache-Control: no-store`. It validates in the spec's order and redirects to `/login` with a one-time notice. `RateLimit(10, 15m)` per IP.
- The "password was reset" security notice and the `user.password.reset` audit entry.

## Acceptance criteria

- [x] The link is a 32-byte random token, only its SHA-256 is stored, and its path is logged as the route pattern.
- [x] `GET` doesn't spend the link, names the account, and sends `Referrer-Policy: no-referrer` and `Cache-Control: no-store`.
- [x] A link works once, only before it expires, and only while it is the newest link for its account.
- [x] A link stops working after a password change, a sign-out-everywhere, or an email change.
- [x] A short, overlong or mismatched new password re-renders the form without spending the link.
- [x] A 2FA account must enter a TOTP or backup code. A wrong code counts against the per-user reauth limit and doesn't spend the link. The next sign-in still asks for TOTP.
- [x] A successful reset sets the new hash, bumps `session_version` (so existing sessions are refused), sets `email_verified_at` if it was unset and the link was emailed, redirects to `/login` with a notice, and signs nobody in.
- [x] Personal access tokens, OAuth app grants and SSH keys keep working, and the notice says so and links to settings.
- [x] `IssueLink` refuses an account without a password.
- [x] A completed reset sends the "password was reset" notice and writes `user.password.reset` with `issued_by` in its metadata.
- [x] `POST /auth/password/reset/{token}` answers `429` past 10 requests per IP per 15 minutes.
- [x] Integration tests cover each spend condition and the race of two concurrent spends.

## Blocked by

None.

## Comments

Claude, 2026-10-06: Done. The spend conditions and the race are tested through `PasswordResetService` (`internal/service/password_reset_service_test.go`), which runs the real store against Postgres, rather than in a separate store test. The full flow, the headers, the old-session and PAT checks, and the rate limits are tested through the router (`internal/router/password_reset_test.go`).
