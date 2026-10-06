# Admin-issued password reset

Created: 2026-10-06
Category: enhancement
Status: ready-for-agent

## Problem

A user who forgets their password on an instance without SMTP has no way back in from the web: the forgot-password form needs outgoing email, and the admin user page (`/admin/users/{username}`, `internal/handler/admin_user_handler.go`) has no reset action. Today the only fix is shell access for `cloudzilla password-reset-link <username>`. Even with SMTP, a user who lost their mailbox can't get the emailed link.

A second gap: the self-service flows ignore suspension. `PasswordResetService.Request` mails a link to a suspended account, and `PasswordResetStore.Lookup`/`Consume` accept it. Suppose an admin suspends an account because its mailbox is compromised. The attacker requests a reset during the suspension, sets a password only they know, and signs in the moment the admin unsuspends. Links issued *before* a suspension are already dead, because `UserStore.Suspend` bumps `session_version`, which `passwordResetUsable` checks.

## Proposed design

### Suspended accounts in the reset flows

- `passwordResetUsable` (`internal/store/password_reset_store.go`) gains `u.suspended_at IS NULL`. `Lookup` and the locked recheck in `Consume` then report a suspended account's link as invalid, without saying why. The cooldown query in `Issue` also stops counting a suspended account's admin link as usable.
- `Request` returns nil without mailing anything (link or passwordless note) when the account is suspended, the same as for an unknown address, so the form doesn't reveal the state.
- `IssueLink` returns `ErrUserSuspended` for a suspended account, so the CLI never prints a link that couldn't work. The CLI says to unsuspend first.

### The admin action

- `POST /api/admin/users/{username}/password-reset-link`, next to the other `/users/{username}/*` actions in `internal/router/router.go`, behind `superadminMW` and the same reauth (`confirmAction`: password, plus 2FA when the admin has it).
- `AdminUserService.IssuePasswordResetLink(ctx, actorID, username)` returns the link. It refuses:
  - the admin's own account: `ErrAdminSelf` (403, "Change your own account under Account settings."), and the button is hidden when `Self`;
  - a suspended account: `ErrUserSuspended` (409, "Unsuspend the account first.");
  - an account without a password: `ErrPasswordResetNoPassword` (409, with a message naming SSO sign-in), and the button is hidden for such accounts.
- It calls `PasswordResetService.IssueLink(ctx, userID, model.PasswordResetByAdmin)`: a 24-hour single-use link (`PasswordResetManualTTL`) that replaces any outstanding link.
- **The link is always shown once to the admin, never emailed**, whether or not SMTP is configured. The response renders a fragment with the link, a copy button, the expiry and "this link won't be shown again". The admin hands it over. It never goes into the audit log, application logs or the page after a reload.
- With SMTP configured, the user gets a security notice through `notifySecurityChange` (as `admin_totp_reset` does): an administrator issued a password reset link for `@user`; it works once within 24 hours; if you didn't ask for this, tell your administrator. The notice doesn't contain the link.
- Audit: reuse `user.password.reset_link` (`model.AuditActionPasswordResetLink`) with the admin as actor, the user as target, and meta `{"issued_by":"admin"}`. The CLI writes the same action with `issued_by: "cli"`. The spend still writes `user.password.reset`. As in the CLI, the entry is recorded before the link is returned: a link that isn't in the audit log is never shown.
- Spending an admin-issued link doesn't verify the email address (#175's rule, already in `Consume`) and never signs anyone in.

## Acceptance criteria

See the tickets in `issues/`.

## Decisions

- **Show once, notify by email.** The action exists for the user whose mail doesn't reach them, so emailing the link would fail in exactly the case it's for. Showing it every time keeps a single behavior, and the notice email means an admin issuing a link behind the user's back gets noticed.
- **24 hours, same as the CLI.** A hand-delivered link often crosses time zones or a night. The link is single-use, replaced by any newer link, still needs the user's 2FA code when they have one, and the notice reports it.
- **Suspended accounts are refused everywhere**, not just in the admin action. Unsuspending doesn't revive old links, because suspension bumps `session_version`.
- **Refused on the admin's own account**, like every other `/admin/users` action: it would be a password change that skips the current-password check in Account settings.
- **One audit action.** `user.password.reset_link` already means "a manual link was issued". The spec's earlier `admin.user.password_reset` is dropped, and `issued_by` tells the CLI and the admin apart.

## Comments

**Malith Kuruppu, 2026-10-06:** Unblocked: password reset (#175) and admin user management (#176) are both on main. `PasswordResetService.IssueLink` already returns a 24-hour link without mailing it, so this action builds on it. The spec's `admin.user.password_reset` audit action should be reconciled with the existing `user.password.reset_link` that the CLI writes. Still to settle before `ready-for-agent`: emailing vs. showing the link when SMTP is on, the link's lifetime, whether the forgot/reset flows should also skip suspended accounts, and confirming the refusal on the admin's own account.

**Malith Kuruppu, 2026-10-06:** Settled: show the link once and email only a notice; keep 24 hours; refuse suspended accounts in the forgot/reset flows and `IssueLink` too; refuse the admin's own account; reuse `user.password.reset_link` with `issued_by: admin`.
