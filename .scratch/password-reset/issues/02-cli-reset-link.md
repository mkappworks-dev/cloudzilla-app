# `cz-admin password-reset-link`

Created: 2026-10-06
Category: enhancement
Status: done

Spec: [../spec.md](../spec.md)

## What to build

A cobra subcommand in `cmd/cz-admin/`, registered next to `gc`, `stats` and `seed`: `cz-admin password-reset-link <username>`. It loads config, connects to the database, calls `PasswordResetService.IssueLink(ctx, userID, "cli")`, and prints the absolute link, built from the configured external URL. It needs no SMTP.

`AuditService.Record` needs a non-nil `*http.Request`, so the CLI writes `user.password.reset_link` through the audit store directly (or through a request-free method on the service), with no actor and the user as target.

## Acceptance criteria

- [x] `cz-admin password-reset-link <username>` prints a link that works for 24 hours, with SMTP unset.
- [x] It exits non-zero with a clear message for an unknown user or an account without a password, and issues nothing.
- [x] Running it again replaces the outstanding link (the old one stops working) and ignores the email cooldown.
- [x] It writes `user.password.reset_link` to the audit log with no actor, the user as target, and `{"issued_by":"cli"}`.
- [x] `docs/access-control.md` documents the command.

## Blocked by

- 01-reset-link-and-form

## Comments

Claude, 2026-10-06: Done. `cmd/cz-admin/password_reset_link.go` writes the audit entry through `AuditService.RecordOffline` and prints the link only once that write succeeds. The audit entry has no actor ID and the actor name `cz-admin`. A CLI link doesn't verify the address.
