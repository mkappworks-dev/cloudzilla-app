# CLI: reset a user's two-factor authentication

Created: 2026-10-06
Category: enhancement
Status: needs-triage

## Problem

A sole superadmin who has lost both their authenticator and their backup codes can't get back in without SQL.

- `POST /api/admin/users/{username}/reset-2fa` (`AdminResetUserTOTP` in `internal/handler/admin_user_handler.go`, `AdminUserService.ResetTOTP` in `internal/service/admin_user_service.go`) clears 2FA for another account. `ResetTOTP` resolves the account through `AdminUserService.target`, which refuses the acting admin's own account (`ErrAdminSelf`). A sole superadmin has no other admin to ask.
- `cloudzilla-cli password-reset-link <username>` (`cmd/cloudzilla/password_reset_link.go`) mints a reset link offline. But the reset form asks a 2FA account for a TOTP or backup code (`ReauthService.CheckSecondFactor`), and sign-in asks again, so the link alone doesn't help someone who lost their authenticator.

Example: the only superadmin of a small instance replaces their phone without moving the authenticator, and the backup codes were never saved. Every sign-in stops at the 2FA prompt, and today the operator's only way back is `UPDATE users SET totp_enabled = false ...` in psql.

## Proposed design

`cloudzilla-cli reset-2fa <username>`, mirroring the admin action for the operator:

- Clears the TOTP secret, flag and backup codes through the same code path as `AdminUserService.ResetTOTP` (`users.SetTOTPEnabled(ctx, id, false, "")` plus `notifySecurityChange(..., "admin_totp_reset", adminTOTPResetNotice)`), not a copy. `ResetTOTP` goes through `target`, which needs an acting admin ID, so the shared part has to be split out (e.g. an unexported method taking the resolved user, called by both `ResetTOTP` and a new offline entry point).
- Mails the same security notice. Its wording says "An administrator turned off…", which still fits an operator.
- Writes the audit entry through `AuditService.RecordOffline` with actor name `cloudzilla-cli` and no actor ID, as `password-reset-link` does. `password-reset-link` records the non-admin action `user.password.reset_link` (`model.AuditActionPasswordResetLink`), so the CLI-specific choice is a new `user.2fa.reset`-style action rather than reusing `admin.user.2fa_reset`. Settle this against how the audit log page renders actions.
- Prints a confirmation only after the audit write succeeds; a failed audit write exits non-zero.
- A user without 2FA enabled: decide whether to no-op with a message or exit non-zero (no audit entry either way if nothing changed).

## Acceptance criteria

- [ ] `cloudzilla-cli reset-2fa <username>` turns off 2FA and clears the secret and backup codes for that account.
- [ ] The admin `reset-2fa` endpoint and the CLI share one implementation of the reset itself.
- [ ] The account's owner gets the same security notice email.
- [ ] An audit entry is written with actor name `cloudzilla-cli` and no actor ID; the confirmation prints only after it succeeds.
- [ ] An unknown username and bad arguments exit non-zero with a clear message; the ghost user is treated as `password-reset-link` treats it.
- [ ] Tests cover the command (as `password_reset_link_test.go` does) and the shared service path.
- [ ] `docs/configuration.md` documents the command next to `password-reset-link`.
- [ ] `docs/access-control.md` documents it under "Two-factor authentication" and gives the full recovery path for a locked-out sole superadmin: `reset-2fa`, then `password-reset-link` if the password is lost too.

## Relevant files

- `cmd/cloudzilla/password_reset_link.go`, `password_reset_link_test.go`, `main.go`
- `internal/service/admin_user_service.go` (`ResetTOTP`, `target`, `adminTOTPResetNotice`)
- `internal/service/audit_service.go` (`RecordOffline`)
- `internal/model/audit_log.go` (action constants)
- `internal/store/admin_user_store.go` (`keepActiveSuperadmin`, `ErrLastSuperadmin`)
- `internal/handler/admin_user_handler.go` (`AdminResetUserTOTP`)
- `docs/configuration.md`, `docs/access-control.md`

## Open questions

Walk through each with the user: concrete problem and example first, then options, then a recommendation.

1. Should the command also bump `session_version`? The admin `reset-2fa` doesn't. Check whether it should, for consistency with other security changes (e.g. `Suspend` bumps it).
2. Should it refuse a suspended account, or should the CLI also get `unsuspend`? `keepActiveSuperadmin` already refuses suspending the last active superadmin (`ErrLastSuperadmin`), so the "only superadmin is suspended" case should be unreachable through the app; verify there's no other path.
3. Should it require a `--yes` flag or an interactive confirmation? Check what `password-reset-link` and other CLI subcommands do.
