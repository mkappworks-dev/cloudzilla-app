# Logs, artifacts and retention

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent
Blocked by: 04
Parallel with: 01

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-18.3-ci-logs-artifacts` | `.worktrees/feat+phase-18.3-ci-logs-artifacts` | `feat(ci): chunked logs, artifacts and retention (18.3)` | L |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/18-ci-pipelines.md`.

## Problem

Jobs run but nobody can read their output, and a build can print hundreds of MB while the storage `Backend` cannot
append. Artifacts have no home, and without retention both fill the disk.

## Scope

- Migration (next free number; `138_create_ci_logs_artifacts.sql` as of the spec): log chunks (job, sequence, line
  offsets, storage key) and artifacts (name, size, SHA-256, key, expiry).
- Log ingest: the runner posts chunks with line offsets; each is written to the `Backend` as an object, then the row,
  rolled back on failure. The 20 MiB per-Job cap writes a single truncation line. Log read by line offset and a
  full-log download that concatenates chunks. Secret masking applied on the runner before upload and again as a
  server-side backstop for the repo's secret values once ticket 06 stores them (design the hook now, with no secrets
  yet).
- Artifacts: the runner uploads each `artifacts:` entry after the Job (archive of the path), capped at 100 MiB each and
  10 per Run, stored under their own key prefix; download through the app with `CanRead` on every request,
  `Content-Disposition: attachment`, `nosniff` and a sandboxing CSP, and the same 404 for every refusal.
- Retention: `ci.log_retention` and `ci.artifact_retention` (30 days), an hourly worker (the attachment sweep's pattern)
  deleting expired objects after the database change commits; orphans are safe to delete and documented. Deleting a
  repo, user or org removes their CI objects (extend the purge and the user-delete collection).
- `docs/storage.md` gains the CI object layout, access rules and cleanup; `docs/configuration.md` the keys.

## Files (hotspots in bold)

- `internal/db/migrations/138_create_ci_logs_artifacts.sql` (new), `internal/model/workflow_log.go` (new)
- **`internal/store/stores.go`**, `internal/store/workflow_log_store.go` (new), **`internal/service/services.go`**,
  `internal/service/workflow_log_service.go` (new), `internal/service/repo_service.go` (purge),
  `internal/service/user_service.go` (delete collection)
- `internal/handler/ci_runner_handler.go`, `internal/handler/workflow_artifact_handler.go` (new),
  **`internal/router/router.go`**, `cmd/server/main.go`, `cmd/cz-runner/`
- `docs/storage.md`, `docs/configuration.md`, `docs/ci.md`

## Acceptance criteria

- [ ] A 30 MiB log is cut at 20 MiB with one truncation line; offsets read back contiguous lines; the full download
      equals the concatenated chunks
- [ ] A failed object write leaves no row; a failed row after a write deletes the object
- [ ] An artifact uploads, downloads with the right headers, and a private repo's artifact returns the same 404 to a
      non-reader as a missing one
- [ ] Oversize and over-count artifacts are refused with the limit named
- [ ] The retention worker deletes expired chunks and artifacts and keeps Run rows and statuses; a repo purge and a
      user delete remove the objects
- [ ] ~~Light and dark checks at 1440~~ (no UI; the UI is ticket 06)
- [ ] ~~Standing mocks in `mockups/` updated~~ (no UI)
- [ ] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` /
      `cloudzilla-seed`) with a real runner: a Job printing 50 MB and uploading an artifact; `curl` reads the log by
      offset and downloads the artifact, evidence in the PR
- [ ] `make lint` and `go test ./...` pass

## Verification

Throwaway server and runner as in ticket 04; `curl -b` with a session for the log and artifact endpoints, signed out for
the 404s.
