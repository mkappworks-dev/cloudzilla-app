# Next coverage gaps

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent

## Problem

Ticket 08 listed the lowest-covered non-generated files after the CI database and LDAPS work. Cover the first group; the rest are out of scope here.

## Acceptance criteria

- [ ] Tests for `handler/activity_handler.go`, `handler/notification_handler.go`, `service/commit_stats_backfill.go`, `service/contributor_stats_service.go` and `service/event_service.go` reach at least 70% each.
- [ ] Tests for the `cz-admin` `backup`, `restore` and `seed` commands (use the CI Postgres 18 and its `pg_dump`/`pg_restore`) reach at least 70% each.
- [ ] `go test -coverprofile -coverpkg=./...` re-run and the next lowest files noted in a comment.

## Out of scope

`cmd/server/main.go`, the view layer (render-smoke tests) and the `sso_handler.go` success branches that need a real LDAP server or IdP.

## Blocked by

Nothing.
