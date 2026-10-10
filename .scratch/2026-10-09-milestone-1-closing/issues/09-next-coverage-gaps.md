# Next coverage gaps

Created: 2026-10-09
Category: enhancement
Status: done

## Problem

Ticket 08 listed the lowest-covered non-generated files after the CI database and LDAPS work. Cover the first group; the rest are out of scope here.

## Acceptance criteria

- [x] Tests for `handler/activity_handler.go`, `handler/notification_handler.go`, `service/commit_stats_backfill.go`, `service/contributor_stats_service.go` and `service/event_service.go` reach at least 70% each.
- [x] Tests for the `cz-admin` `backup`, `restore` and `seed` commands (use the CI Postgres 18 and its `pg_dump`/`pg_restore`) reach at least 70% each. _The CI database has no `pg_dump`/`pg_restore` (see ticket 08), so the command tests use stand-in scripts and reach 93–100% without the real tools; the real round trip skips without them._
- [x] `go test -coverprofile -coverpkg=./...` re-run and the next lowest files noted in a comment.

## Out of scope

`cmd/server/main.go`, the view layer (render-smoke tests) and the `sso_handler.go` success branches that need a real LDAP server or IdP.

## Blocked by

Nothing.

## Comments

Claude, 2026-10-10: Done. Coverage with `go test -coverprofile -coverpkg=./... ./...` against a migrated scratch Postgres 18 (statement coverage, blocks merged across packages), before → after: `handler/activity_handler.go` 0% → 97%, `handler/notification_handler.go` 50% → 96%, `service/commit_stats_backfill.go` 0% → 97%, `service/contributor_stats_service.go` 38% → 97%, `service/event_service.go` 46% → 100%, `cmd/cz-admin/restore.go` 19% → 93%, `cmd/cz-admin/backup.go` 19% → 100%, `cmd/cz-admin/seed.go` 35% → 100%. The cz-admin figures come from stand-in `pg_dump`/`pg_restore` scripts, so they hold in any environment with `TEST_DATABASE_DSN`. The tests add about 8 s to `cmd/cz-admin` and run in CI's Integration job (the seed uses 3 users, 1 org, 2 repos); the real round trip, `TestBackupThenRestore_WithTheRealPostgresTools`, skips without major-18 `pg_dump` and `pg_restore` on `PATH` and passed against the Postgres 18 client tools.

The activity tests found a bug: `PageActivity` asked the store for `pageSize+1` rows to detect a next page, but the store derives its offset from that size, so page 2 started at row 11 and the 11th, 21st, … events never appeared on any page. Fixed in its own commit (the store's feed lists take `limit, offset`; `EventService.FeedPage` reads one row past the page).

Next lowest non-generated files (excluding `internal/view` and `cmd/server/main.go`): `cmd/gen-code-themes/main.go` 0% (5 statements), `cmd/cz/open.go` 0% (28), `handler/audit_handler.go` 0% (14), `handler/markdown_handler.go` 0% (8), `handler/render_helpers.go` 38% (21), `cmd/cz/main.go` 43% (14), `cmd/cz-admin/password_reset_link.go` 47% (30), `handler/home_page_handler.go` and `handler/base_page.go` (combined) 48% (221), `cmd/cz-admin/main.go` 48% (21), `handler/profile_readme_handler.go` 50% (48), `handler/settings_handler.go` 50% (44), `service/explore_service.go` 50% (18).
