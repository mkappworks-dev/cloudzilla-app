# Admin action: issue a password reset link

Created: 2026-10-06
Category: enhancement
Status: ready-for-agent

Spec: [../spec.md](../spec.md)

## What to build

- `AdminUserService.IssuePasswordResetLink(ctx, actorID, username) (*model.User, string, error)`: refuses `ErrAdminSelf`, `ErrUserSuspended` and `ErrPasswordResetNoPassword`, calls `PasswordResetService.IssueLink(..., model.PasswordResetByAdmin)`, and sends an "administrator issued a reset link" security notice (no link in it) through `notifySecurityChange`.
- `AdminIssuePasswordResetLink` handler on `POST /api/admin/users/{username}/password-reset-link`: superadmin, reauth through `confirmAction`, records `user.password.reset_link` with `{"issued_by":"admin"}` before returning the link, and renders a fragment that shows the link once. Unlike the other admin actions, it doesn't reload the page.
- A "Password reset link" row in the Actions card on `/admin/users/{username}`, hidden for the admin's own account (the Actions card already is), for suspended accounts, and for accounts without a password.
- `docs/access-control.md`: the endpoint in the admin endpoint matrix, and the admin path in the password-reset section.

## Acceptance criteria

- [ ] The action needs the admin's password (and 2FA code when the admin has it). A wrong confirmation issues nothing.
- [ ] A non-superadmin gets 403.
- [ ] The admin's own account gets 403 and no link. A suspended account gets 409. An account without a password gets 409. Each issues nothing.
- [ ] A successful issue returns a working `/auth/password/reset/{token}` link that expires in 24 hours, replaces any outstanding link, and is shown once with a copy button. Reloading the page doesn't show it again.
- [ ] Spending it sets the password, doesn't verify the email, signs nobody in, and writes `user.password.reset` with `issued_by: admin`.
- [ ] `user.password.reset_link` is recorded with the admin as actor, the user as target and `{"issued_by":"admin"}`. The link never appears in the audit log or application logs.
- [ ] With SMTP, the user gets a notice that names no link. Without SMTP, the action still works.
- [ ] The row isn't rendered for a suspended or passwordless account.
- [ ] `docs/access-control.md` lists the endpoint and describes the admin path.

## Blocked by

- 01-suspended-accounts-in-reset-flows.md
