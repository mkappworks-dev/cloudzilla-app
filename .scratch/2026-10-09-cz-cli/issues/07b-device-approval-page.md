# Device login: approval page, confirmation and audit

Created: 2026-10-09
Category: enhancement
Status: done
Blocked by: 07a

Part of [07](./07-device-code-login.md); read its Design section first. The browser half: where a signed-in user enters the code and approves.

## What to build

- `GET /login/device` (`optAuthMW`; signed-out users go to `/login?next=…` and come back to the empty code-entry page; the code is never carried in a URL), `POST /login/device` (code entry), `POST /login/device/approve`. Handler in `internal/handler/` (every `Page*` method goes in `<concern>_page_handler.go`; JSON and form-`POST` handlers stay in `<concern>_handler.go`), view-model in `internal/view/viewmodels_*.go`, Templ in `internal/view/pages/`, routes in `internal/router/router.go`. Run `make generate-templ`.
- Code entry is limited to 50 per hour per user; an unknown, expired or used code gets one generic message.
- The confirm step is one page, top to bottom:
  1. Header "Authorize cz" with the signed-in username.
  2. A warning banner: "Only continue if you just ran `cz auth login` and this code matches your terminal."
  3. A details table: code, device name (tagged "unverified"), requester IP and time.
  4. "Access this token will have": each scope a bordered, pre-ticked row with its description; the user may untick but not add, and must keep at least one. A hint says so.
  5. "Confirm it's you": labelled fields for only the factors the account has (password, authenticator code), with the provider, email-code and backup alternatives from `components.ConfirmFactors`.
  6. One primary **Authorize cz** button and a **Deny** button beside it, then a line saying the token can be revoked under Settings → Tokens.

  The page sends `X-Frame-Options: DENY` and `frame-ancestors 'none'`. The approval controls are real labelled form fields and buttons, not just text.
- Approving calls `Reauth.Confirm` through `confirmationFrom(r)` and `reauthRefusal`, as `ConfirmAuthorize` does, and renders `components.ConfirmFactors` for password, 2FA, LDAP, Google/SAML and email-code accounts. The provider sign-in round-trip must return to the confirm step with the grant intact; keep it server-side or in a short-lived signed cookie, never in a URL. Denying needs no confirmation.
- Audit events `user.device.approve`, `user.device.deny` (add to `internal/model/audit_log.go`); 07a's token mint writes `user.token.create`. Approval mails the account a notice (device name, IP, scopes).
- `docs/access-control.md`: the flow, the confirmation rules, the threat model from 07 (including the phishing residual risk), the new endpoints in the authorization matrix. `docs/api-reference.md`: the three browser routes.

## Acceptance criteria

- [x] A signed-out visit to `/login/device` signs in and lands on the empty code-entry page.
- [x] `GET /login/device?user_code=…` ignores the parameter: the field stays empty and nothing is looked up.
- [x] Approve fails with a wrong password or code (`403`), is throttled after five failures (`429`), and the grant stays pending; deny works without confirmation and kills the grant.
- [x] A user with 2FA completes an approval end to end; a Google/SAML-only user and an LDAP user can confirm.
- [x] Unticking scopes narrows the issued token; a forged extra scope in the form is ignored; `repo:admin` cannot be added.
- [x] Entry beyond 50 per hour per user gets `429`.
- [x] The approval writes the audit events and sends the notice; the device name is escaped on the page and in the email.
- [x] The confirm page cannot be framed.
- [x] Tests cover each case above; the docs are updated.

## Comments

2026-10-09, hand-off from the first 07b session. Built and reviewed on `feat/cz-device-approval-page`: the three pages, the four routes plus `GET /login/device/approve` (a 303 to the confirm page, so a provider sign-in can return), the server-signed and user-bound `cz_device_code` cookie, the 50-per-hour per-user entry limiter, audit events, the approval notice, and the docs. Left for the next session:

- Test the two still-uncovered confirmation paths through the device page: an LDAP account (directory password) and a real Google or SAML round trip (`cz_reauth`). Emailed-code and 2FA confirmation are covered. These are the unticked boxes above; set `Status: done` when both are ticked.
- Provider-only accounts: **Authorize** stays enabled until they confirm, so the first click spends one of the five shared reauth attempts. Consider disabling it until `ProviderReady` or an email code is entered.
- Clear `cz_device_code` at sign-out. It is now user-bound and signed, so this is tidiness only.
- Tell the user when the cookie's grant has expired or was already answered (today it is a silent 303 to `/login/device`); show the viewer's own IP beside the requester's; label the device name "unverified" in the email.
- Test debt: the 410 in-window race branch; `SameSite` and `MaxAge` on the cookie; the token-refusal test covers 2 of the 4 routes; `NotifyApproved` and the limiter's no-claims case; replace `time.Sleep(200ms)` before zero-count audit assertions with a bounded wait; assert the grant stays `denied` after an approve-after-deny.
- Docs: the threat-model row "code cookie planted from a sibling subdomain" predates the signing and understates it (a planted cookie cannot be forged and is bound to the attacker's own user id); the 410 behaviour is not in `api-reference.md`; "Settings, Tokens" and "Access tokens" name the same page.
- The token handler does an extra `GetByID` just for the audit actor name; `Poll` could return the username.

2026-10-09, second 07b session. Done on `feat/cz-device-approval-page`:

- Tests: an LDAP account confirms with its directory password through the device page (`testutil.FakeLDAP` answers binds that carry the right password). For Google/SAML the provider itself can't be faked, so the test covers our side of the round trip: `POST /settings/reauth/google` with `return_to=/login/device/confirm` redirects to Google and remembers the confirm page; a code from the real `BeginProviderSignIn` and `FinishGoogleSignIn` in `cz_reauth` approves; another account's code and a spent code get `403`. SAML shares that `cz_reauth` path (`FinishSAMLSignIn`) but isn't run through the device page; the callbacks themselves are covered in `internal/handler/provider_signin_test.go`.
- **Authorize** is disabled for a provider account until it signs in again or types an emailed code (Alpine on the form, `ConfirmFactors.AwaitingProvider`), so the first click no longer spends a shared reauth attempt. **Deny** stays enabled.
- A signed cookie whose grant is gone gets `410` and the "expired or was already answered" message; the confirm page shows the viewer's IP beside the requester's; the approval email calls the device name unverified; sign-out clears `cz_device_code`.
- Test debt: the in-window `410` race (a stepping service clock lands the request between the lookups), cookie `SameSite` and `MaxAge`, token refusal on all five routes, the limiter with no claims, the unnamed-device notice, the grant staying `denied` after approve-after-deny, and bounded waits (`assertNoAudit`) in place of the 200ms sleeps.
- Docs: threat-model row for the planted cookie, the `410` behaviour in `api-reference.md`, and "Access tokens" as the one name for the tokens page.
- Left alone: the extra `GetByID` in the token handler. `Poll` would have to do the same lookup, so moving it saves nothing.
