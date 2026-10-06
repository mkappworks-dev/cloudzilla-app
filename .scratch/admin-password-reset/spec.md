# Admin-issued password reset

Created: 2026-10-06
Category: enhancement
Status: needs-triage

## Problem

A user who forgets their password on an instance without SMTP has no way back in: the forgot-password flow on `feat/password-reset` emails its link, and the admin user page from [../admin-user-management/spec.md](../admin-user-management/spec.md) has no reset action. Today the only fix is SQL.

## Proposed design

An action on `/admin/users/{username}` that issues a reset token through the password-reset branch's service, not a new token table. With SMTP, it emails the link. Without SMTP, it shows the link once for the admin to hand over, as invitations do. The action needs the admin's password and 2FA code, is refused on the admin's own account and on a suspended user, and writes an `admin.user.password_reset` audit entry. A reset must not sign a suspended user in.

## Open questions

- Should the shown-once link expire sooner than a self-service reset link?

## Comments

**Malith Kuruppu, 2026-10-06:** Unblocked: password reset (#175) and admin user management (#176) are both on main. `PasswordResetService.IssueLink` already returns a 24-hour link without mailing it, so this action builds on it. The spec's `admin.user.password_reset` audit action should be reconciled with the existing `user.password.reset_link` that the CLI writes. Still to settle before `ready-for-agent`: emailing vs. showing the link when SMTP is on, the link's lifetime, whether the forgot/reset flows should also skip suspended accounts, and confirming the refusal on the admin's own account.
