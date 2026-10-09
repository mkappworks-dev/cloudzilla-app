# Remaining test gaps

Created: 2026-10-09
Category: enhancement
Status: done

## Problem

After the coverage rounds (#186, #188, #204) the weakest remaining areas are:

- **Backup and restore against real tools.** `internal/backup` tests use stand-in `pg_dump` and `pg_restore` scripts because the dev machine has neither; the original round-trip tests skip there. Unknown whether the CI runner has them, and the stand-ins do not check that the real flags work.
- **LDAP over TLS.** `bindLDAP` with `use_tls=true` is untested. The code uses system roots with `MinVersion` TLS 1.2 and cannot be pointed at a test CA.
- **Google OAuth callback past the state check.** `handler.UseFakeGoogle` is test-only inside the handler package, so router tests stop at the state check (see ticket 03).
- **`cmd/server/main.go`** (173 statements, 0%): startup wiring, config errors, graceful shutdown.
- **View layer.** `view/fragments` ~6% and `view/pages` ~23% are mostly generated templ code; `model` is 17%. Render-smoke tests that execute each page with representative data would catch template panics.
- Handlers still under 70%: `page_handler.go`, `page_repo_handler.go`, `page_pull_handler.go`, `sso_handler.go` success branches (need a real LDAP server or IdP).

## Acceptance criteria

- [x] Check whether CI has `pg_dump`/`pg_restore`; if not, add them to the test job so the existing round-trip tests run.
- [x] Add an injectable TLS config (or root pool) to `bindLDAP` and test the TLS path with a self-signed CA.
- [x] Move the fake Google hook somewhere router tests can reach it.
- [x] Re-run `go test -coverprofile` with `-coverpkg=./...` and pick the next lowest non-generated files.

## Blocked by

Ticket 03 for the OAuth part.

## Comments

Claude, 2026-10-10: Triaged as an enhancement. Ticket 03 is done, and `handler.UseFakeGoogle` already lives in `fake_google.go`; `internal/router/google_oauth_routes_test.go` uses it, and `TestGoogleOAuthRoutes_CallbackWithMatchingStateSignsIn` already drives the callback past the state check, so no new router test was needed.

CI had neither the tools nor a database: the test job ran `go test ./...` with no `TEST_DATABASE_DSN`, so every integration test, the backup round trips included, skipped on the runner. A new `Integration` job, parallel to `Test` so the fast job stays fast and `Build` waits for both, starts a `postgres:18-alpine` service, installs `postgresql-client-18` from PGDG (the round-trip tests refuse a client older than the server), migrates the test database with `cz-admin migrate`, and sets `TEST_DATABASE_DSN`. Before pushing, the full suite passed against a migrated scratch Postgres 18, and both `TestBackupRestore_*` round trips passed with the real Postgres 18 `pg_dump`/`pg_restore`. The first CI run is the only check of the apt and service wiring itself.

`bindLDAP` verifies LDAPS certificates against `ldapRootCAs` (nil means system roots). `TestBindLDAP_TLS` serves a fake LDAP server behind a self-signed CA and checks that an unknown CA is refused, a trusted one binds, and a plaintext bind fails. The diff touches only `bindLDAP` and its imports, so ticket 04's split of `sso_service.go` can carry it along.

Coverage (`-coverpkg=./...`, merged, excluding generated templ, `internal/view` and `cmd/server/main.go`), lowest first: `cmd/cz/open.go` 0%, `handler/activity_handler.go` 0%, `service/commit_stats_backfill.go` 0%, `handler/notification_handler.go` 13%, `cmd/cz-admin/restore.go` 19%, `cmd/cz-admin/backup.go` 19%, `cmd/cz-admin/seed.go` 35%, `handler/render_helpers.go` 38%, `service/contributor_stats_service.go` 38%, `handler/page_handler.go` 43%, `service/event_service.go` 46%, `cmd/cz-admin/password_reset_link.go` 47%. The remaining items in the Problem list (`main.go`, the view layer, the handler success branches that need a real LDAP/IdP) are left to ticket 09.
