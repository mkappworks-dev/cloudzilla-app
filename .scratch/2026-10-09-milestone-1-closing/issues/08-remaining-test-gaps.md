# Remaining test gaps

Created: 2026-10-09
Category: enhancement
Status: needs-triage

## Problem

After the coverage rounds (#186, #188, #204) the weakest remaining areas are:

- **Backup and restore against real tools.** `internal/backup` tests use stand-in `pg_dump` and `pg_restore` scripts because the dev machine has neither; the original round-trip tests skip there. Unknown whether the CI runner has them, and the stand-ins do not check that the real flags work.
- **LDAP over TLS.** `bindLDAP` with `use_tls=true` is untested. The code uses system roots with `MinVersion` TLS 1.2 and cannot be pointed at a test CA.
- **Google OAuth callback past the state check.** `handler.UseFakeGoogle` is test-only inside the handler package, so router tests stop at the state check (see ticket 03).
- **`cmd/server/main.go`** (173 statements, 0%): startup wiring, config errors, graceful shutdown.
- **View layer.** `view/fragments` ~6% and `view/pages` ~23% are mostly generated templ code; `model` is 17%. Render-smoke tests that execute each page with representative data would catch template panics.
- Handlers still under 70%: `page_handler.go`, `page_repo_handler.go`, `page_pull_handler.go`, `sso_handler.go` success branches (need a real LDAP server or IdP).

## Acceptance criteria

- [ ] Check whether CI has `pg_dump`/`pg_restore`; if not, add them to the test job so the existing round-trip tests run.
- [ ] Add an injectable TLS config (or root pool) to `bindLDAP` and test the TLS path with a self-signed CA.
- [ ] Move the fake Google hook somewhere router tests can reach it.
- [ ] Re-run `go test -coverprofile` with `-coverpkg=./...` and pick the next lowest non-generated files.

## Blocked by

Ticket 03 for the OAuth part.
