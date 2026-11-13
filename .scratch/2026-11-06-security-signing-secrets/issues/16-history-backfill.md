# History backfill and blocked-alert sweep

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent
Blocked by: 10, 11, 12
Parallel with: 13, 14, 15

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-17.3-history-backfill` | `.worktrees/feat+phase-17.3-history-backfill` | `feat(secrets): scan existing history (17.3)` | L |

Spec: [spec-secret-scanning.md](../spec-secret-scanning.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/17-secret-scanning.md`.

## Problem

The feature ships on repositories with years of commits. Without a history scan the Security tab would be empty on every existing repo until someone pushed a secret again.

## Scope

- Migration (next free) with `repo_secret_scans` as in the spec, `pending` rows inserted for every existing repository and for each new one at creation (`repo_service_create.go`, import and mirror creation).
- `SecretScanService.RunBackfill(ctx)` started from `cmd/server/main.go` beside the mirror scheduler (lease-based claim as in `mirror_scheduler.go`; multiple instances never scan one repo twice): scan every reachable blob once (visited set), within `secret_scanning.backfill_timeout` (5 min) and `backfill_max_blobs` (200,000), record alerts as `found` with `detail = 'history scan'`, `reached_ref = true`, revoke live tokens through ticket 14 when merged (else record the finding and leave the token), and send one `NotifySecretAlert` digest per repo.
- `MarkPending(repoID)` called when a pull mirror sync finishes (`MirrorService`) and an import completes; the sweep deletes `blocked`-only alerts older than 7 days each loop.
- The Security page shows "Scanning existing history…" for a repo whose scan is not `done`, and "History scan incomplete" when `incomplete` is set (view-model field and one banner; mock both).
- Config `secret_scanning.{backfill_timeout,backfill_max_blobs}`; documented in `docs/configuration.md`.

## Files (hotspots in bold)

- `internal/db/migrations/NNN_create_repo_secret_scans.sql` (new), **`cmd/server/main.go`**, `internal/service/secret_scan_service.go` (new), `internal/service/secret_backfill_scheduler.go` (new)
- **`internal/service/repo_service_create.go`**, `internal/service/mirror_service.go`, `internal/service/import_service.go`, **`internal/config/config.go`**
- `internal/view/pages/security_secrets.templ`, **`internal/view/viewmodels_security.go`**, `mockups/security_secrets.html`

## Acceptance criteria

- [ ] A repo with a key in an old commit on a non-default branch gets one alert, one `found` event and one admin digest after the job runs; a second run adds nothing
- [ ] A file unchanged across 10,000 commits is read once (benchmark or counter assertion); a budget overrun marks the scan `incomplete` and the next run does not restart from scratch needlessly
- [ ] Two scheduler instances on one database never claim the same repo (lease test)
- [ ] A mirror sync and an import re-queue the scan; `blocked`-only alerts past 7 days are swept
- [ ] The banners render for `pending`, `running` and `incomplete`
- [ ] Checked at desktop 1440 in **light and dark** on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`); evidence for both themes in the PR
- [ ] The standing mocks in `mockups/` listed under Files are updated in the same PR (classes copied from the templ, `COVERAGE.md` / `index.html` / `shell.js` where affected) and checked under `make mockups` in both themes
- [ ] Verified on a throwaway server (scratch Postgres on a free port, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`) with native git; what was seen is in the PR
- [ ] `make generate-templ` re-run and the `_templ.go` files committed (if `.templ` changed); `make lint` (`/Users/mk/go/bin/golangci-lint run`) and `go test ./...` pass

## Verification

Throwaway server: restore or create a repo whose history holds a key (push it with protection off, or seed with native git into the bare repo), start the server and watch the job; check the Security page banner, the alert and the admin digest in light and dark at 1440.

## Comments

