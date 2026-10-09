# User page, suspend and unsuspend, revoke tokens and keys

Created: 2026-10-06
Category: enhancement
Status: done

Spec: [../spec.md](../spec.md)

Blocked by: 01, 03, 04

## What to build

`GET /admin/users/{username}`: a details card (email and verification, role, status, 2FA, sign-in methods, created date, last web sign-in from the latest `login` audit entry, organizations the user solely owns) and an actions card. Unknown or ghost usernames are `404`.

A new `AdminUserService` holds the actions. Each is a `POST /api/admin/users/{username}/…`, confirmed with `h.confirmAction` (password and 2FA code via `ConfirmFields` in the global `ConfirmDialog`), refused on the acting admin's own account, and audited with the admin as actor and the user as target:

- `suspend` (optional `reason`): sets `suspended_at` and bumps `session_version` in one `UPDATE`, under the active-superadmin lock. The dialog names organizations the user solely owns. Audit `admin.user.suspend` with the reason.
- `unsuspend`: clears `suspended_at`. Audit `admin.user.unsuspend`.
- `revoke-credentials`: deletes the user's PATs, SSH keys and OAuth app authorizations and bumps `session_version` in one transaction; mails a security notice. Audit `admin.user.credentials_revoke` with the counts.

The public profile (`user.templ`) shows a "Suspended" badge to superadmins only.

## Acceptance criteria

- [x] `/admin/users/{username}` shows the account's details and actions; an unknown or ghost username is a `404`.
- [x] Suspension ends existing sessions, which stay dead after unsuspension.
- [x] Revoke tokens and keys deletes the user's PATs, SSH keys and OAuth app authorizations, ends their sessions, and mails the user; deploy keys are untouched.
- [x] The suspend dialog names the organizations the user solely owns.
- [x] Each action refuses the acting admin's own account, needs the admin's password (and 2FA code), and writes its audit entry.
- [x] The profile's "Suspended" badge shows only to superadmins.

## Tests

- Store: suspend bumps `session_version`; revoke deletes the three credential kinds and leaves deploy keys.
- Service: self is refused; the last active superadmin can't be suspended; audit entries are written.
- Handler: suspend without the password is refused; with it, the user is suspended; the user page `404`s for the ghost.
