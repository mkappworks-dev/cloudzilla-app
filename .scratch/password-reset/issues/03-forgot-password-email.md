# Forgot-password form and emails

Created: 2026-10-06
Category: enhancement
Status: ready-for-agent

Spec: [../spec.md](../spec.md)

## What to build

The self-service entry point.

- A **Forgot password?** link on `/login`, shown only when `PasswordResetService.Available()` (SMTP on). It needs a flag on `LoginData`. Get the user's pick on the layout mock-up before building it.
- `GET`/`POST /auth/password/forgot`. `POST` validates the address with `validEmail` and always renders the same "check your inbox" page. `Request(ctx, email)` runs in `concurrency.Go`: it sends nothing for an unknown address, a 1-hour link for an account with a password (verified or not), and the "signs in with …" note for an account without one. With SMTP off, both methods render "ask an administrator" and send nothing. `RateLimit(10, 15m)` per IP, with its own budget.
- The 5-minute per-account cooldown lives in the store's upsert, so it holds across instances and covers the passwordless note too.
- The link email and the passwordless note, in `EmailService`.

## Acceptance criteria

- [ ] With SMTP on, `/login` links to `/auth/password/forgot`. With SMTP off it doesn't, and the forgot page says to ask an administrator.
- [ ] The forgot form returns the same page, status and timing for a registered address, an unregistered one and a passwordless one. Only a malformed address gets a different response (the form error).
- [ ] A registered address gets one email per 5 minutes at most, however many requests arrive, across instances.
- [ ] An unverified address gets a link like a verified one.
- [ ] An account without a password gets the "signs in with Google / single sign-on" note and never a link.
- [ ] An emailed link expires after 1 hour.
- [ ] `POST /auth/password/forgot` answers `429` past 10 requests per IP per 15 minutes.
- [ ] `docs/access-control.md` and `docs/configuration.md` describe the flow, routes and limits.

## Blocked by

- 01-reset-link-and-form
