# `cloudzilla-cli password-reset-link`

Created: 2026-10-06
Category: enhancement
Status: ready-for-agent

Spec: [../spec.md](../spec.md)

## What to build

A cobra subcommand in `cmd/cloudzilla/`, registered next to `gc`, `stats` and `seed`: `cloudzilla-cli password-reset-link <username>`. It loads config, connects to the database, calls `PasswordResetService.IssueLink(ctx, userID, "cli")`, and prints the absolute link, built from the configured external URL. It needs no SMTP.

`AuditService.Record` needs a non-nil `*http.Request`, so the CLI writes `user.password.reset_link` through the audit store directly (or through a request-free method on the service), with no actor and the user as target.

## Acceptance criteria

- [ ] `cloudzilla-cli password-reset-link <username>` prints a link that works for 24 hours, with SMTP unset.
- [ ] It exits non-zero with a clear message for an unknown user or an account without a password, and issues nothing.
- [ ] Running it again replaces the outstanding link (the old one stops working) and ignores the email cooldown.
- [ ] It writes `user.password.reset_link` to the audit log with no actor, the user as target, and `{"issued_by":"cli"}`.
- [ ] `docs/access-control.md` documents the command.

## Blocked by

- 01-reset-link-and-form
