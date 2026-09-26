# 2026-09-26 — Email-verified signup

**Status:** Design approved; not yet implemented.
**Branch:** `fix/invite-register-email-disclosure`
**Affected subsystems:** registration (`internal/handler/register_handler.go`, new `signup_handler.go`), users (`internal/store/user_store.go`), new `SignupTokenStore` / `SignupService`, email (`internal/service/email_service.go`), router, Templ pages under `internal/view/pages/`.

---

## Background

`POST /register` creates the account immediately, so its response differs between a new email (account created, signed in) and a registered one (generic "Could not create account"). Anyone can use that to learn whether an email has an account. Earlier commits on this branch hid the DB error text and added a per-IP rate limit (10 POSTs per 15 minutes), which slows bulk probing but still answers a single lookup.

Closing the leak needs the response to be the same either way, which means the account can't be created until the mailbox owner acts. That requires email, so the fix applies only when SMTP is configured.

## Locked decisions

1. **Email first.** With SMTP configured, `/register` asks only for an email. The username and password are chosen on a page reached from the emailed link, so nobody can pre-set a password for someone else's address.
2. **`signup_tokens` table.** Links are single-use, expire, can be revoked, and are stored as a hash. Invitations are not reused: invited accounts get `is_invited`, which bypasses `allow_login`.
3. **No SMTP, no change.** Without `smtp.host`, `/register` keeps the current full form behind the rate limit. The docs state that only the SMTP mode stops `/register` from revealing whether an email is registered.

---

## Flow

With SMTP configured (`SignupService.Enabled()`):

| Route | Behaviour |
| --- | --- |
| `GET /register` | Email-only form. |
| `POST /register` | Validates the email's shape (`net/mail.ParseAddress` accepts it and it has no display name), starts `SignupService.Request` in the background, and renders the "Check your inbox" page. The status, body and timing don't depend on whether the email has an account. |
| `GET /register/complete/{token}` | Usable link: completion form with the email read-only, plus username and password (8+ characters). Otherwise: the generic "This link is no longer valid" page. |
| `POST /register/complete/{token}` | Creates the account, sets the auth cookie, and redirects to `/`. A taken username re-renders the form with "That username is already taken" and leaves the link usable. An unusable link gets the generic invalid page. |

Both modes:

- `allow_registration=false` redirects every one of these routes to `/login`, as `/register` does today. That includes the completion routes, so closing registration also stops outstanding links.
- `POST /register` and `POST /register/complete/{token}` carry `middleware.RateLimit(accountCreationLimit, accountCreationWindow)`.
- Accounts created by signup are ordinary users: `is_invited = FALSE`.

What `Request` sends:

- **New address:** a link to `{server.base_url}/register/complete/{token}`, valid for 24 hours.
- **Address with an account:** "You already have a Cloudzilla account. Sign in at `{server.base_url}/login`. If you didn't ask for this, ignore this email."

## Data

Migration `075_signup_tokens.sql`:

```sql
CREATE TABLE IF NOT EXISTS signup_tokens (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    token_hash  TEXT        NOT NULL UNIQUE,
    email       TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS signup_tokens_email_lower_key ON signup_tokens (lower(email));
```

- `token_hash` is the hex SHA-256 of a 32-byte random token. The raw token appears only in the email.
- Each email has at most one row. A new request replaces the row, and with it any earlier link.
- Model: `model.SignupToken{ID, Email, ExpiresAt}` in `internal/model/`.

## Components

### `SignupTokenStore`

- `Issue(ctx, email, tokenHash, expiresAt) (issued bool, err error)`: a single upsert that also throttles.

  ```sql
  INSERT INTO signup_tokens (token_hash, email, expires_at) VALUES ($1, $2, $3)
  ON CONFLICT (lower(email)) DO UPDATE
     SET token_hash = EXCLUDED.token_hash, email = EXCLUDED.email,
         created_at = NOW(), expires_at = EXCLUDED.expires_at, used_at = NULL
   WHERE signup_tokens.created_at < NOW() - INTERVAL '5 minutes'
  RETURNING id
  ```

  No row returned means the address was issued a link within the last 5 minutes, so `issued=false`. The throttle is atomic and holds across instances.
- `GetUsableByHash(ctx, tokenHash) (*model.SignupToken, error)` returns `ErrSignupTokenUnusable` unless `used_at IS NULL AND expires_at > NOW()` and no user has `lower(email) = lower(signup_tokens.email)`. That predicate is a shared constant, as `usableInvitationCond` is for invitations.

### `UserStore.CreateFromSignupToken(ctx, u, tokenHash) error`

Mirrors `CreateFromInvitation`. In one transaction it claims the token with `UPDATE signup_tokens SET used_at = NOW() WHERE token_hash = $1 AND <usable predicate> RETURNING email`, sets `u.Email` from the returned email, and runs `insertUser`.

- Zero rows claimed returns `ErrSignupTokenUnusable`.
- `ErrEmailTaken` from the insert (an account registered between claim and insert) also returns `ErrSignupTokenUnusable`.
- Any failure rolls back, leaving the link usable.

### `SignupService`

Depends on `SignupTokenStore`, `UserStore`, a `signupMailer`, `server.base_url`, and a clock/TTL (24h).

- `Enabled() bool` reports whether an SMTP host is configured.
- `Request(ctx, email) error` is synchronous; the handler runs it via `concurrency.Go`. It generates the token and calls `Issue`. If throttled, it returns nil. Otherwise it sends `SendAccountExists` when the email has an account (`GetByEmail`, case-insensitive), or `SendSignupLink` with the completion URL when it doesn't. Every error is returned to the caller for logging.
- `GetUsable(ctx, token) (*model.SignupToken, error)` hashes the token, then calls `GetUsableByHash`.
- `Complete(ctx, token, username, password) (*model.User, error)` hashes the password and calls `CreateFromSignupToken`.
- `ErrSignupTokenUnusable` is re-exported from the store, like `ErrInvitationUnusable`.

```go
type signupMailer interface {
	SendSignupLink(to, link string) error
	SendAccountExists(to, loginURL string) error
}
```

`EmailService` implements both methods, HTML-escaping the interpolated values. Tests pass a fake that records calls.

### Handlers and views

- `register_handler.go`: `PageRegister` and `PageRegisterSubmit` branch on `Signup.Enabled()`. The existing full-form path is unchanged.
- `signup_handler.go`: `PageRegisterComplete` and `PageRegisterCompleteSubmit`, structured like the invite handlers: a `usableSignupToken` helper that writes the response when it returns false, a generic invalid page, `createAccountErrorMessage`, and `logCreateAccountFailure`.
- Templ pages (run `make generate-templ`):
  - `RegisterEmail`: the email-only form.
  - `RegisterCheckInbox`: the same page for every submit.
  - `RegisterComplete`: the completion form, or the invalid-link hero when there's no token.
- View-models go in `viewmodels_auth.go`.
- Routes go in `router.go`. `/register/complete/` is not reachable before setup completes, so `middleware/setup.go` stays unchanged.

## Errors and logging

| Case | User sees | Log |
| --- | --- | --- |
| Email empty or malformed | Form error "Enter a valid email address" (input-only) | none |
| Throttled | "Check your inbox" | none |
| Token issue or DB error in `Request` | "Check your inbox" | Error, no address |
| SMTP send fails | "Check your inbox" | Error, no address |
| Completion: username taken | Form, "That username is already taken", link still usable | Info |
| Completion: other create failure | Form, generic create-account message | Error |
| Completion: link unusable | Generic invalid page | none |

## Security properties

- `POST /register` gives the same status, body and timing for new and existing emails, because the work runs in the background.
- Only someone with the mailbox can set the account's password.
- Links are single-use, expire after 24 hours, are stored hashed, and are superseded by the next request for the same address.
- Mail volume is capped at one per address per 5 minutes (DB-backed) and 10 requests per IP per 15 minutes (per process).

## Testing

TDD, using the same patterns as this branch (`testutil.OpenTestDB`, `testutil.SeedUser`, `UniqueSuffix`, `testutil.Exec` cleanups).

- **Store/service:**
  - `Issue` twice within 5 minutes: the second returns `issued=false`.
  - The raw token is never stored, only its hash.
  - A token for an existing email is not usable.
  - An expired token is not usable.
  - A claimed token makes `Complete` return `ErrSignupTokenUnusable` and create no user.
  - A taken username leaves the token usable.
- **Service with a fake mailer:**
  - A new email gets `SendSignupLink`, with a link that starts with `base_url` and whose token redeems.
  - An existing email (in any case) gets `SendAccountExists`.
  - A throttled request sends nothing.
- **Handlers (SMTP enabled via the fake):**
  - The `POST /register` bodies are byte-identical for a new and an existing email.
  - Completion GET/POST covers the invite matrix: usable, used, expired, unknown and already-registered links; a taken username keeps the link usable; concurrent submits create one account.
  - `allow_registration=false` redirects.
- **Handlers (SMTP disabled):** the existing register tests keep passing.
- **Migration:** a `internal/db` test that the `lower(email)` index rejects a second row for the same address in another case.

## Out of scope

- Password reset, and verification of email changes on existing accounts.
- Verification for OAuth, LDAP and SAML signups; those providers vouch for the email.
- A shared (multi-instance) store for the per-IP rate limit.
