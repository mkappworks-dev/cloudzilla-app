# Refuse suspended accounts in the password reset flows

Created: 2026-10-06
Category: enhancement
Status: ready-for-agent

Spec: [../spec.md](../spec.md)

## What to build

- `passwordResetUsable` in `internal/store/password_reset_store.go` adds `u.suspended_at IS NULL`, which covers `Lookup`, the locked recheck in `Consume` and the admin-link check in `Issue`'s cooldown.
- `PasswordResetService.Request` sends nothing for a suspended account and returns nil.
- `PasswordResetService.IssueLink` returns `ErrUserSuspended` for a suspended account. `cloudzilla password-reset-link` turns it into "@user is suspended: unsuspend it first".

## Acceptance criteria

- [ ] `Request` for a suspended account's address sends no email (no link and no passwordless note) and returns nil.
- [ ] A link issued while the account is active, then the account is suspended, stays invalid after unsuspending (existing `session_version` bump; covered by a test).
- [ ] A link row for a suspended account (inserted directly) reads as invalid from `Check`, and `Reset` refuses it without changing the password.
- [ ] Suspending the account between `Check` and `Reset` makes `Reset` refuse the link, through `Consume`'s recheck under lock.
- [ ] `IssueLink` returns `ErrUserSuspended` for a suspended account, and the CLI prints an unsuspend hint.
- [ ] `docs/access-control.md`'s password-reset section says suspended accounts get no reset.

## Blocked by

None.
