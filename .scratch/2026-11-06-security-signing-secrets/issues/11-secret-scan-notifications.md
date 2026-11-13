# Secret scan notifications and audit

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent
Blocked by: 10
Parallel with: 12, 13

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-17.3-secret-scan-notifications` | `.worktrees/feat+phase-17.3-secret-scan-notifications` | `feat(secrets): notify admins of secret scan alerts (17.3)` | M |

Spec: [spec-secret-scanning.md](../spec-secret-scanning.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/17-secret-scanning.md`.

## Problem

Repo admins hear nothing when a secret is found after the fact or when a writer allows one, and the owner of an auto-revoked token would learn only when their next request fails.

## Scope

- Migration widening `notifications_type_check` (re-read its latest definition, migration 099) with `secret_alert`, `secret_allowed`, `token_revoked`; matching `model.Notif*` constants (the `notification_test.go` check that constants and the constraint agree must pass).
- `RepoService`/store lookup of a repo's admin recipients: every user for whom `CanManage` is true (owner; org owners of an org repo; `admin` collaborators; users with `admin` through a team grant, see [access-control](../../docs/access-control.md)). Read the team-grant code from #326 and reuse its resolution rather than re-deriving it.
- `NotificationService.NotifySecretAlert(ctx, repo, alert)`, `NotifySecretAllowed(ctx, repo, alert, actor)` (skips the actor, as `actorID == authorID` does elsewhere) and `NotifyTokenRevoked(ctx, owner, token, repo)`: in-app plus `sendEmailAsync`. `NotifySecretAlert` takes a count so a backfill sends one digest per repo.
- Audit: `secret_scan.close`, `secret_scan.reopen`, `secret_scan.allow` recorded through `AuditService` by the ticket-12 handlers; `token.auto_revoke` (system actor) by ticket 14. This ticket adds the action names and a helper, and documents them in `audit_log` docs if the page lists actions.
- Notification and mail text never contain the secret or its hint beyond the type and location.

## Files (hotspots in bold)

- `internal/db/migrations/NNN_secret_scan_notifications.sql` (new), **`internal/model/notification.go`**
- **`internal/service/notification_service.go`**, `internal/service/notification_service_secret_test.go` (new)
- `internal/store/` (recipient query), `internal/service/audit_service.go`

## Acceptance criteria

- [ ] A repo with an owner, an org owner, an `admin` collaborator, a `writer` and a team-granted admin notifies exactly the admins and not the writer
- [ ] A digest for 7 findings creates one notification, not seven; an allow does not notify its actor
- [ ] The notification list renders the three new types without a fallback label (check `notifications` page labels and the mail templates)
- [ ] Notification and email bodies contain no secret hint (test)
- [ ] ~~Checked at desktop 1440 in light and dark~~ (no UI in this ticket)
- [ ] ~~The standing mocks in `mockups/` are updated~~ (no UI in this ticket)
- [ ] Verified on a throwaway server (scratch Postgres on a free port, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`) with native git; what was seen is in the PR
- [ ] `make lint` (`/Users/mk/go/bin/golangci-lint run`) and `go test ./...` pass

## Verification

Integration tests; then on the throwaway server trigger each notification through a test hook or ticket 13/14 if merged and read the notification list and the mail log.

## Comments

