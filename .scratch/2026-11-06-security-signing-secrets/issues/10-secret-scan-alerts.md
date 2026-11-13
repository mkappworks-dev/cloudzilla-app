# Secret scan alerts: tables, store, service

Created: 2026-11-13
Category: enhancement
Status: done
Blocked by: 09
Parallel with: none

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-17.3-secret-scan-alerts` | `.worktrees/feat+phase-17.3-secret-scan-alerts` | `feat(secrets): secret scan alerts and lifecycle (17.3)` | L |

Spec: [spec-secret-scanning.md](../spec-secret-scanning.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/17-secret-scanning.md`.

## Problem

A finding has to be stored, deduped and closed with a reason. ROADMAP's single `state` column cannot say why an alert is closed, and a blocked push needs an alert id before any ref has moved.

## Scope

- Migration (next free; 132 as of 79f4ef23) with `secret_scan_alerts`, `secret_scan_locations`, `secret_scan_events` exactly as in the spec's Schema section; `model.SecretScanAlert`, `…Location`, `…Event`; `SecretAlertStore` wired into `Stores` (`internal/store/stores.go`).
- `SecretAlertService` (wired into `Services`): `Record(ctx, repoID, finding, location, kind)` creates the alert (next per-repo `number` in the same transaction) or appends a location and event to the existing `(repo_id, secret_hash)` alert, returning the alert and whether it is new; `Close(ctx, alertID, actor, resolution)`, `Reopen`, `Allow(ctx, alertID, actor, resolution)` (only `false_positive`/`used_in_tests`, refused when `secret_type = cloudzilla_token`); `MarkReached(ctx, repoID, commitSHAs)` sets `reached_ref`; `IsClosed(ctx, repoID, hash)`; `List(ctx, repoID, state, secretType, page)` and `Get(ctx, repoID, number)`; `OpenCount` (excludes alerts whose locations all have `reached_ref = false`); `SweepBlocked(ctx, olderThan)`.
- Every method takes `ctx` first and enforces nothing about the actor beyond recording it: authorisation is the handler's (`CanWrite`, ticket 12).
- Closing sets `state`, `resolution`, `resolved_by`, `resolved_at` and an event; reopening clears them. Concurrent `Record` calls for the same hash must produce one alert (the unique index plus `ON CONFLICT`).
- Cascade: deleting a repository removes its alerts, locations and events; deleting a user nulls `resolved_by` and event `actor_id`.

## Files (hotspots in bold)

- `internal/db/migrations/NNN_create_secret_scan_alerts.sql` (new), `internal/model/secret_scan.go` (new)
- **`internal/store/stores.go`**, `internal/store/secret_alert_store.go` (new)
- **`internal/service/services.go`**, `internal/service/secret_alert_service.go` (new)

## Acceptance criteria

- [x] Store integration tests (`TEST_DATABASE_DSN`): create, dedupe on the same hash, location append, the `state`/`resolution` CHECK, cascade, per-repo numbering under concurrent inserts
- [x] Service tests: close/reopen/allow transitions and events, `Allow` refused for a Cloudzilla token alert, `OpenCount` ignoring blocked-only alerts, `SweepBlocked` deleting only alerts with no `reached_ref` location older than the cutoff
- [x] `make migrate` applies the migration on a fresh and on an existing database
- [ ] ~~Checked at desktop 1440 in light and dark~~ (no UI in this ticket)
- [ ] ~~The standing mocks in `mockups/` are updated~~ (no UI in this ticket)
- [x] Verified on a throwaway server (scratch Postgres on a free port, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`) with native git; what was seen is in the PR (no route or git path exists yet: `cz-admin migrate` and `\d secret_scan_alerts` checked on a scratch Postgres, on a database already at 131 then migrated)
- [x] `make lint` (`/Users/mk/go/bin/golangci-lint run`) and `go test ./...` pass

## Verification

Integration tests against a scratch Postgres (`make test-integration`); then `make migrate` on the throwaway database and a `\d secret_scan_alerts` to confirm the constraints.

## Comments

