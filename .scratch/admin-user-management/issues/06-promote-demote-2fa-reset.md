# Promote, demote and 2FA reset

Created: 2026-10-06
Category: enhancement
Status: done

Spec: [../spec.md](../spec.md)

Blocked by: 02, 03, 05

## What to build

Three more actions on the user page, same pattern as ticket 05:

- `promote`: `is_superadmin = TRUE`; refused while the user is suspended. Audit `admin.user.promote`.
- `demote`: `is_superadmin = FALSE` under the active-superadmin lock; refused on self. Audit `admin.user.demote`.
- `reset-2fa`: clears `totp_secret`, `totp_enabled` and `totp_backup_codes`; refused on self; mails a security notice. Audit `admin.user.2fa_reset`.

## Acceptance criteria

- [x] Promote and demote take effect on the user's next request, without signing them out.
- [x] Reset 2FA clears the secret, flag and backup codes, and mails the user.
- [x] No action applies to the acting admin's own account (except promote, which is moot).

## Tests

- Service: promote refuses a suspended user; demote refuses self and the last active superadmin; reset clears the TOTP columns.
- Handler: demote with confirmation flips the role.
