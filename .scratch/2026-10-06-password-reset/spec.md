# Password reset

Created: 2026-10-06
Category: enhancement
Status: done

## Problem

A user who forgets their password has no way back in. Nothing in the code offers a reset. There is no route, no token table, and no link on `/login` (`internal/view/pages/login.templ` links only to `/register`). Changing the password (`POST /settings/password`) needs the current one. No admin tool sets or resets another user's password either: `/admin/*` and `/api/admin/*` cover settings, invitations, manual email verification and SSO. The only way out today is editing `users.password_hash` in the database.

Most of what a reset needs already exists:

- **SMTP:** Phase 11.1. `EmailService.Enabled()` is `smtp.host != ""`.
- **Hashed, single-use link tokens:**
  - `signup_tokens` (migration 083): one row per address, and a new request replaces the row. The `Issue` upsert doubles as a 5-minute per-address cooldown.
  - `email_verification_tokens` (092): 32 `crypto/rand` bytes, base64url, SHA-256 at rest. GET only shows the link and POST spends it, because mail scanners fetch links.
- **Ending sessions:** `users.session_version` (093). `UserStore.ChangePassword` swaps the hash and bumps the version in one compare-and-set `UPDATE`.
- **Security notices:** `notifySecurityChange` + `passwordChangedNotice` mail the account in the background and ignore notification preferences.
- **No account enumeration:** `requestSignup` answers every valid address with the same page and mails from `concurrency.Go`, so neither the response nor its timing shows whether an account exists.
- **Rate limiting:** `middleware.RateLimit` (per IPv4 address or IPv6 /64, in-process) wraps `POST /login`, `/api/auth/login` and `/register`: 10 per 15 minutes for account creation, 30 for login.
- **TOTP checks:** `ReauthService.CheckSecondFactor` checks a TOTP or backup code under the per-user limit of 5 failures per 15 minutes.
- **Audit entries:** `AuditService.Record` accepts `actorID = 0`. `VerifyEmailSubmit` shows an unauthenticated route that records the token's user as the actor.
- **Token paths stay out of logs:** `middleware.Logger` logs a route's pattern instead of its path when the pattern contains `{token}`.

## Proposed design

### Flow

1. `/login` shows **Forgot password?** when the instance can send email.
2. `GET /auth/password/forgot` asks for an email address. `POST` validates it with `validEmail` (from the signup handler), then always renders the same "check your inbox" page. A background job (`concurrency.Go`) then looks the address up and:
   - **No account:** sends nothing.
   - **Account with a password:** issues a link and mails it.
   - **Account without a password:** mails a note saying how the account signs in (see [Accounts without a password](#accounts-without-a-password)).
3. `GET /auth/password/reset/{token}` checks the link without spending it. It shows a form naming the account (`@username`, email), with new password and confirmation fields. When 2FA is on, the form also has a TOTP or backup code field.
4. `POST /auth/password/reset/{token}` checks things in this order:
   1. Password length (8 characters to 72 bytes, `MinPasswordLen`/`MaxPasswordBytes`) and that the confirmation matches. A typo spends nothing.
   2. The TOTP code, if one is required.
   3. One transaction that spends the link, sets the new hash and bumps `session_version`.

   It then redirects to `/login` with a one-time "Password changed, sign in" notice. A reset never signs anyone in: sign-in stays the one path that applies `allow_login`, TOTP and the `login` audit entry.

The routes live under `/auth/`, which is already a reserved owner name. A new top-level segment such as `/password` would shadow an existing user or org named `password`, along with its repos named `forgot` and `reset`.

### Tokens

New table `password_reset_tokens` (next free migration number at commit time; `102` as of `4d1e48d7`):

| Column                | Notes                                                                      |
| --------------------- | -------------------------------------------------------------------------- |
| `user_id`             | `UNIQUE`, `ON DELETE CASCADE`. One row per account, so only the newest link works |
| `email`               | Address the link was sent to                                               |
| `token_hash`          | SHA-256 of 32 `crypto/rand` bytes (base64url in the link). `NULL` when the email carried no link (the passwordless notice) |
| `session_version`     | The user's version at issue                                                |
| `issued_by`           | `email`, `admin` or `cli`, for the audit entry                             |
| `created_at`, `expires_at`, `used_at` |                                                            |

A link is usable only while all of these hold. The spend checks them in the same `UPDATE`/transaction, with the user row locked first (the lock order `EmailVerificationStore` uses):

- `used_at IS NULL` and `expires_at > NOW()`.
- `users.session_version` still equals the row's. So a password change, **Sign out other sessions**, or another reset kills outstanding links.
- `lower(users.email) = lower(email)`. So an email change kills them too.
- `users.password_hash <> ''`.

Lifetimes:
- An emailed link lasts **1 hour**.
- A link issued by an admin or the CLI lasts **24 hours**, because it is passed on by hand.

Per-account cooldown: at most **one email per 5 minutes**. It is enforced in the store's upsert, like `SignupTokenStore.Issue`, so it holds across instances, and it covers the passwordless note too. Admin and CLI issuance skip the cooldown and replace any outstanding link.

`newVerificationToken`/`hashVerificationToken` move to neutrally named shared helpers rather than being copied.

### Reusable service

`PasswordResetService` owns issuing and spending. The admin-user-management work can call it directly:

- `Request(ctx, email)`: the background job behind the forgot form.
- `IssueLink(ctx, userID, issuedBy) (url string, err error)`: for an admin or CLI fallback. It returns the link instead of mailing it, and refuses an account without a password.
- `Check(ctx, token)` / `Reset(ctx, token, newPassword, code)`: back the GET and POST.
- `Available() bool`: whether the instance can send email.

### Sessions and other credentials

The spend bumps `session_version`, so every session ends, including any an attacker held. Personal access tokens, OAuth app grants and SSH keys keep working (question 1).

### Two-factor authentication

A reset replaces only the password. TOTP stays on, and the next sign-in still asks for a code. The reset form also asks for one (question 2).

### Accounts without a password

Google-, LDAP- and SAML-created accounts store `password_hash = ''`. LDAP accounts usually carry a made-up `<username>@ldap.local` address (`sso_ldap.go`), so mail to them goes nowhere. A reset never adds a password to them (question 3). The note they get names how the account signs in: Google, or single sign-on.

### Without SMTP

- `/login` shows no **Forgot password?** link.
- `GET`/`POST /auth/password/forgot` render "This instance can't send email. Ask an administrator for a reset link." and send nothing.
- The admin fallback is `cz-admin password-reset-link <username>` (question 4).

### Notices

- **Link email:** subject "Reset your Cloudzilla password". It names `@username`, says the link works once and expires in 1 hour, and ends: "If you didn't ask for this, ignore this email. Your password hasn't changed."
- **Passwordless note:** "Your Cloudzilla account @alice signs in with Google, so it has no password to reset."
- **After a reset:** a "Your Cloudzilla password was reset" variant of `passwordChangedNotice`, sent through `notifySecurityChange`. It says every session was signed out, and that tokens, SSH keys and authorized apps still work, with a link to their settings.

### Audit log

- On success, write `user.password.reset` (new `model.AuditActionPasswordReset`). Actor and target are the user, as in `VerifyEmailSubmit`, with metadata `{"issued_by": "email"|"admin"|"cli"}`.
- Issuing a link from the admin fallback writes `user.password.reset_link`, with the admin as actor (or none, for the CLI) and the user as target.
- Anonymous requests on the forgot form aren't audited. The response doesn't depend on the account, and the rate limits bound the volume.

### Rate limits

- `POST /auth/password/forgot`: `RateLimit(10, 15m)` per client IP, with its own budget, plus the per-account 5-minute cooldown.
- `POST /auth/password/reset/{token}`: `RateLimit(10, 15m)` per client IP. Wrong TOTP codes also count against the per-user reauth limit.

### Unverified addresses

Every account created before migration 091 starts unverified, and so does every account from the classic no-SMTP `/register`. Refusing resets to unverified addresses would therefore strand most users of an upgraded instance: they can't sign in to verify. So the link goes to unverified addresses too, and a completed reset sets `email_verified_at` (in the same transaction as the new hash), since only the mailbox's owner could have opened it.

### Unchanged

- `internal/middleware/setup.go`: before setup no account exists, so there's nothing to reset.
- `internal/middleware/scope.go`: these are HTML routes outside `authMW`, already closed to tokens.
- CSRF: the forms carry `csrf_token` like `/register`.
- `allow_login`: the reset ignores it, and sign-in applies it.

## Acceptance criteria

Items marked (Qn) follow the answer to that open question (see Comments).

- [x] With SMTP on, `/login` links to `/auth/password/forgot`. With SMTP off, it doesn't, and the forgot page says to ask an administrator.
- [x] The forgot form returns the same page, status and timing for a registered address, an unregistered one and a passwordless one. Only a malformed address gets a different response (the form error).
- [x] A registered address with a password gets one email per 5 minutes at most, however many requests arrive, across instances.
- [x] The link is a 32-byte random token. Only its SHA-256 is stored, and its path is logged as the route pattern.
- [x] `GET` on a link doesn't spend it, names the account, and sends `Referrer-Policy: no-referrer` and `Cache-Control: no-store`.
- [x] A link works once, only within 1 hour (24 hours when issued by an admin or the CLI), and only for the newest link.
- [x] A link stops working after a password change, a sign-out-everywhere, or an email change.
- [x] A short, overlong or mismatched new password re-renders the form without spending the link.
- [x] A successful reset sets the new hash, bumps `session_version` (so every existing session is refused), redirects to `/login` with a notice, and signs nobody in.
- [x] A 2FA account still needs its TOTP code at the next sign-in. (Q2) The reset form also requires a TOTP or backup code; a wrong one counts against the per-user reauth limit and doesn't spend the link.
- [x] (Q1) Personal access tokens, OAuth app grants and SSH keys keep working. The notice says so and links to settings.
- [x] (Q3) An account without a password gets the "signs in with …" note, never a link. `IssueLink` refuses it.
- [x] (Q4) `cz-admin password-reset-link <username>` prints a 24-hour link, works without SMTP, refuses an account without a password, and writes `user.password.reset_link` to the audit log.
- [x] (Q5) Unverified addresses can reset, and a completed reset from an emailed link sets `email_verified_at`.
- [x] A completed reset mails the "password was reset" notice and writes `user.password.reset` to the audit log.
- [x] `POST /auth/password/forgot` and `POST /auth/password/reset/{token}` answer `429` past 10 requests per IP per 15 minutes.
- [x] `docs/access-control.md` and `docs/configuration.md` describe the flow, routes and limits.

## Relevant files

- `internal/db/migrations/` — new `password_reset_tokens` migration
- `internal/store/signup_token_store.go`, `internal/store/email_verification_store.go` — upsert-cooldown and lock-order precedents; new `internal/store/password_reset_store.go`, wired in `stores.go`
- `internal/store/user_store_auth.go` — `ChangePassword`, `BumpSessionVersion`
- `internal/store/user_store.go` — `GetByEmail` (skips the ghost)
- `internal/service/signup_service.go`, `internal/service/email_verification_service.go` — token helpers to share; new `internal/service/password_reset_service.go`, wired in `services.go`
- `internal/service/email_service.go`, `internal/service/security_notice.go` — link email and reset notice
- `internal/service/reauth_service.go` — `CheckSecondFactor`, `Factors`
- `internal/service/user_service.go` — `MinPasswordLen`, `MaxPasswordBytes`, `hashPassword`
- `internal/handler/signup_handler.go` — `requestSignup`, `validEmail`; new `internal/handler/password_reset_handler.go`
- `internal/handler/email_verification_handler.go` — `renderVerifyEmail` headers, audit precedent
- `internal/handler/page_auth_handler.go` — `PageLogin`, `PageLoginSubmit`, `signIn`
- `internal/view/pages/login.templ`, `internal/view/viewmodels_auth.go` — link and `LoginData` flag; new forgot/reset pages
- `internal/model/audit_log.go` — new action constants
- `internal/router/router.go` — routes and rate limits
- `cmd/cz-admin/` — if the admin fallback includes a CLI command
- `docs/access-control.md`, `docs/configuration.md`

## Open questions

1. **Revoke tokens, OAuth grants and SSH keys on reset?** Recommendation: no. Revoke sessions only, matching `ChangePassword` and the documented split ("Personal access tokens, SSH keys and OAuth app grants are not sessions; they are revoked in their own sections"). None of them depends on the password, and a forgotten password isn't a sign of compromise, so revoking would break CI and git remotes for a routine reset. The notice lists what still works and links to settings.
2. **Ask for the TOTP code on the reset form too?** Recommendation: yes, for 2FA accounts (a TOTP or backup code). Without it, someone who controls only the mailbox can change the password and sign the owner out everywhere, even though they still can't sign in. The owner then has to reset back. A 2FA user who forgot only the password still has the code. One who lost both needs the admin fallback either way.
3. **Accounts without a password (Google, LDAP, SAML)?** Recommendation: never reset or add a password. Mail a note naming the sign-in method instead. A local password on an SSO account would survive deprovisioning at the IdP, and settings already refuses to add one for the same reason.
4. **Admin fallback when SMTP is off?** Options:
   - (a) A CLI command (`cz-admin` subcommand) that prints a 24-hour link for a username.
   - (b) A "Generate reset link" action for superadmins in the admin UI, copied and passed on like an invite.
   - (c) Both.

   Recommendation: (a) here, plus `IssueLink` for (b) in the admin-user-management work. (a) is the only fallback that rescues a sole superadmin who forgot their own password, and the admin UI for managing users belongs to that session.
5. **Reset to an unverified address, and verify it on success?** Recommendation: allow it, and set `email_verified_at` when the reset completes. Only the mailbox's owner could open the link, which is the reason a signup link already starts its account verified. Refusing would lock out every account created before migration 091 or through the classic no-SMTP `/register`.

## Comments

Claude, 2026-10-06: The user chose the recommended option for questions 1–4:

1. A reset ends sessions only. The notice says personal access tokens, SSH keys and authorized apps still work, and links to their settings.
2. 2FA accounts must also enter a TOTP or backup code on the reset form.
3. Accounts without a password get a "signs in with …" note and never a link. `IssueLink` refuses them.
4. The admin fallback is a CLI subcommand, `cz-admin password-reset-link <username>`, which prints a 24-hour link. The admin UI button is left to the admin-user-management work, which can call `IssueLink`. A user who lost their authenticator is out of scope.

Claude, 2026-10-06: The user chose the recommended option for question 5: unverified addresses get the link, and a completed reset sets `email_verified_at`. An address that is already verified keeps its original timestamp.

Claude, 2026-10-06: UI picks. **Forgot password?** goes right-aligned on the Password label row of `/login`. The forgot, "check your inbox" and reset pages reuse the login/register auth card: heading, one short paragraph, fields, a full-width button, and a "Back to sign in" link. The reset page names the account under its heading.

Claude, 2026-10-06: Built on `feat/password-reset`. Changes from the design above:

- Q5 applies to emailed links only. A CLI or admin link is handed over by hand, so spending it proves nothing about the mailbox. Verifying from it would let an account registered with someone else's unverified address capture that person's later Google sign-in (found in the security review).
- The cooldown uses a separate `emailed_at` column, so a manual link doesn't reset it. While a manual link is still usable, the forgot form sends nothing, so nobody can cancel that link with a form request.
- The audit entry from the CLI has no actor ID and the actor name `cz-admin`, written through the new `AuditService.RecordOffline`.
