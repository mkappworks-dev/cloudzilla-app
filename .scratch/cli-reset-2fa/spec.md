# CLI: reset a user's two-factor authentication

Created: 2026-10-06
Category: enhancement
Status: done

## Problem

A sole superadmin who has lost both their authenticator and their backup codes can't get back in without SQL.

- `POST /api/admin/users/{username}/reset-2fa` (`AdminResetUserTOTP` in `internal/handler/admin_user_handler.go`, `AdminUserService.ResetTOTP` in `internal/service/admin_user_service.go`) clears 2FA for another account. `ResetTOTP` resolves the account through `AdminUserService.target`, which refuses the acting admin's own account (`ErrAdminSelf`). A sole superadmin has no other admin to ask.
- `cz-admin password-reset-link <username>` (`cmd/cz-admin/password_reset_link.go`) mints a reset link offline. But the reset form asks a 2FA account for a TOTP or backup code (`ReauthService.CheckSecondFactor`), and sign-in asks again, so the link alone doesn't help someone who lost their authenticator.

Example: the only superadmin of a small instance replaces their phone without moving the authenticator, and the backup codes were never saved. Every sign-in stops at the 2FA prompt, and today the operator's only way back is `UPDATE users SET totp_enabled = false ...` in psql.

## Proposed design

`cz-admin reset-2fa <username>`, mirroring the admin action for the operator:

- Clears the TOTP secret, flag and backup codes through the same code path as `AdminUserService.ResetTOTP` (`users.SetTOTPEnabled(ctx, id, false, "")` plus `notifySecurityChange(..., "admin_totp_reset", adminTOTPResetNotice)`), not a copy. `ResetTOTP` goes through `target`, which needs an acting admin ID, so the shared part has to be split out (e.g. an unexported method taking the resolved user, called by both `ResetTOTP` and a new offline entry point).
- Mails the same security notice. Its wording says "An administrator turned off…", which still fits an operator.
- Writes the audit entry through `AuditService.RecordOffline` with actor name `cloudzilla-cli` and no actor ID, as `password-reset-link` does. It reuses `admin.user.2fa_reset`: the audit log page shows the raw action string and filters on it exactly, and `user.password.reset_link` is likewise one action for the admin and CLI paths, told apart by the actor.
- Prints a confirmation only after the audit write succeeds; a failed audit write exits non-zero.
- A user without 2FA enabled: prints that 2FA isn't on and exits 0, with no audit entry and no notice.
- The notice is sent synchronously: `notifySecurityChange` mails from a `concurrency.Go` goroutine, which the CLI would exit before. A failed send is a warning on stderr, not a failure, since the reset already stands. The notice goes out even when the audit write fails, since 2FA is off either way.

## Acceptance criteria

- [x] `cz-admin reset-2fa <username>` turns off 2FA and clears the secret and backup codes for that account.
- [x] The admin `reset-2fa` endpoint and the CLI share one implementation of the reset itself.
- [x] The account's owner gets the same security notice email.
- [x] An audit entry is written with actor name `cloudzilla-cli` and no actor ID; the confirmation prints only after it succeeds.
- [x] An unknown username and bad arguments exit non-zero with a clear message; the ghost user is treated as `password-reset-link` treats it.
- [x] Tests cover the command (as `password_reset_link_test.go` does) and the shared service path.
- [x] `docs/configuration.md` documents the command next to `password-reset-link`.
- [x] `docs/access-control.md` documents it under "Two-factor authentication" and gives the full recovery path for a locked-out sole superadmin: `reset-2fa`, then `password-reset-link` if the password is lost too.

## Relevant files

- `cmd/cz-admin/password_reset_link.go`, `password_reset_link_test.go`, `main.go`
- `internal/service/admin_user_service.go` (`ResetTOTP`, `target`, `adminTOTPResetNotice`)
- `internal/service/audit_service.go` (`RecordOffline`)
- `internal/model/audit_log.go` (action constants)
- `internal/store/admin_user_store.go` (`keepActiveSuperadmin`, `ErrLastSuperadmin`)
- `internal/handler/admin_user_handler.go` (`AdminResetUserTOTP`)
- `docs/configuration.md`, `docs/access-control.md`

## Decisions

Agreed with the maintainer on 2026-10-06.

1. **No `session_version` bump**, as for the admin `reset-2fa` and self-service `TOTPService.Disable`. A bump would also revoke any outstanding password reset link (`password_reset_tokens.session_version`), so `password-reset-link` followed by `reset-2fa` would silently kill the link. The docs point to **Sign out other sessions** once the user is back in.
2. **Suspended accounts are reset, with a note** that the account is still suspended. Suspension only happens through `AdminUserService.Suspend` → `UserStore.Suspend`, which calls `keepActiveSuperadmin`, so a suspended sole superadmin isn't reachable through the app. No `unsuspend` command.
3. **No confirmation and no `--yes`**, like `password-reset-link`: running the CLI already takes the config and database credentials, and the user can re-enroll.

## Plan

1. `security_notice.go`: split the body of `notifySecurityChange` into `sendSecurityNotice(ctx, ...) error`; `notifySecurityChange` runs it in `concurrency.Go` and logs its error.
2. `AdminUserService`: unexported `resetTOTP(ctx, u)` holds the clear; `ResetTOTP` calls it and the async notice. New `ResetTOTPOffline(ctx, username) (*model.User, bool, error)` looks the user up (ghost and unknown → `sql.ErrNoRows`), returns `false` without changes when 2FA is off, else calls `resetTOTP`. New `SendTOTPResetNotice(ctx, userID) error` mails `adminTOTPResetNotice` synchronously.
3. `cmd/cz-admin/reset_2fa.go`: `reset-2fa <username>` → `ResetTOTPOffline`, `RecordOffline` with `admin.user.2fa_reset`, `SendTOTPResetNotice` (warning on failure), then the confirmation and, for a suspended account, a note. Register in `main.go`.
4. Tests first: service test for `ResetTOTPOffline` (clears, reports unchanged for no-2FA, unknown user); CLI test as in `password_reset_link_test.go` (output, audit row, no-2FA path writes no row, unknown user, suspended note).
5. Docs: CLI reference in `docs/configuration.md`; `docs/access-control.md` under Two-factor authentication with the full sole-superadmin recovery path, plus the `reset-2fa` row in Managing accounts.
