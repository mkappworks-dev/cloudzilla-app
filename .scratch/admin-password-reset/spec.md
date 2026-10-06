# Admin-issued password reset

Created: 2026-10-06
Category: enhancement
Status: needs-triage
Blocked by: `feat/password-reset` merging to main

## Problem

A user who forgets their password on an instance without SMTP has no way back in: the forgot-password flow on `feat/password-reset` emails its link, and the admin user page from [../admin-user-management/spec.md](../admin-user-management/spec.md) has no reset action. Today the only fix is SQL.

## Proposed design

An action on `/admin/users/{username}` that issues a reset token through the password-reset branch's service, not a new token table. With SMTP, it emails the link. Without SMTP, it shows the link once for the admin to hand over, as invitations do. The action needs the admin's password and 2FA code, is refused on the admin's own account and on a suspended user, and writes an `admin.user.password_reset` audit entry. A reset must not sign a suspended user in.

## Open questions

- Should the shown-once link expire sooner than a self-service reset link?
