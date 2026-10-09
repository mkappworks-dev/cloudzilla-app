# Device login: approval page, confirmation and audit

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent
Blocked by: 07a

Part of [07](./07-device-code-login.md); read its Design section first. The browser half: where a signed-in user enters the code and approves.

## What to build

- `GET /login/device` (`optAuthMW`; signed-out users go to `/login?next=…` and come back with the code preserved), `POST /login/device` (code entry), `POST /login/device/approve`. Handler in `internal/handler/` (a full page goes in `page_*_handler.go`), view-model in `internal/view/viewmodels_*.go`, Templ in `internal/view/pages/`, routes in `internal/router/router.go`. Run `make generate-templ`.
- Code entry is limited to 50 per hour per user; an unknown, expired or used code gets one generic message.
- The confirm step shows the device name (marked unverified), requester IP and time, and the scopes as pre-ticked boxes; the user may untick but not add, and must keep at least one. The page sends `X-Frame-Options: DENY` and `frame-ancestors 'none'`.
- Approving calls `Reauth.Confirm` through `confirmationFrom(r)` and `reauthRefusal`, as `ConfirmAuthorize` does, and renders `components.ConfirmFactors` for password, 2FA, LDAP, Google/SAML and email-code accounts. The provider sign-in round-trip must return to the confirm step with the user code intact. Denying needs no confirmation.
- Audit events `user.device.approve`, `user.device.deny` (add to `internal/model/audit_log.go`); 07a's token mint writes `user.token.create`. Approval mails the account a notice (device name, IP, scopes).
- `docs/access-control.md`: the flow, the confirmation rules, the threat model from 07 (including the phishing residual risk), the new endpoints in the authorization matrix. `docs/api-reference.md`: the three browser routes.

## Acceptance criteria

- [ ] A signed-out visit to `/login/device?user_code=…` signs in and lands on the confirm step with the code kept.
- [ ] Approve fails with a wrong password or code (`403`), is throttled after five failures (`429`), and the grant stays pending; deny works without confirmation and kills the grant.
- [ ] A user with 2FA completes an approval end to end; a Google/SAML-only user and an LDAP user can confirm.
- [ ] Unticking scopes narrows the issued token; a forged extra scope in the form is ignored; `repo:admin` cannot be added.
- [ ] Entry beyond 50 per hour per user gets `429`.
- [ ] The approval writes the audit events and sends the notice; the device name is escaped on the page and in the email.
- [ ] The confirm page cannot be framed.
- [ ] Tests cover each case above; the docs are updated.

## Comments
