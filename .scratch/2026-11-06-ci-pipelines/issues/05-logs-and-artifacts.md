# Logs, artifacts and retention

Created: 2026-11-13
Category: enhancement
Status: done
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

- [x] A 30 MiB log is cut at 20 MiB with one truncation line; offsets read back contiguous lines; the full download
      equals the concatenated chunks
- [x] A failed object write leaves no row; a failed row after a write deletes the object
- [x] An artifact uploads, downloads with the right headers, and a private repo's artifact returns the same 404 to a
      non-reader as a missing one
- [x] Oversize and over-count artifacts are refused with the limit named
- [x] The retention worker deletes expired chunks and artifacts and keeps Run rows and statuses; a repo purge and a
      user delete remove the objects
- [x] ~~Light and dark checks at 1440~~ (no UI; the UI is ticket 06)
- [x] ~~Standing mocks in `mockups/` updated~~ (no UI)
- [x] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` /
      `cloudzilla-seed`) with a real runner: a Job printing 50 MB and uploading an artifact; `curl` reads the log by
      offset and downloads the artifact, evidence in the PR
- [x] `make lint` and `go test ./...` pass

## Verification

Throwaway server and runner as in ticket 04; `curl -b` with a session for the log and artifact endpoints, signed out for
the 404s.

## Comments

- Claude, 2026-11-13: done, migration 138. Evidence (real runner, real Docker, 50 MB of output, `curl` by offset and the
  artifact download, the signed-out 404s) is in `/private/tmp/claude-501/ci-ticket-05-evidence.md`; copy it into the PR.
  - A fix for ticket 04 went in first (`4909a3de`): the runner now starts one container per Job and runs each Step with
    `docker exec`, so a file made in one Step is there in the next. A Step's `timeout-minutes` kills that Step's processes
    (`kill -9 -1` inside the container, which spares PID 1) and the Job carries on; the Job timeout removes the container.
  - For ticket 06 (UI): `WorkflowLogService.ReadLog(ctx, jobID, offset, limit)` returns lines from a line offset and
    `next_offset`; the JSON form is `GET /api/repos/{owner}/{repo}/actions/runs/{number}/jobs/{jobID}/logs?offset=&limit=`
    (`complete` is true once the Job has finished, so poll until a page is empty and `complete`). A Step's line range is
    `workflow_steps.log_first_line` (inclusive) to `log_last_line` (exclusive). Full log:
    `GET /{owner}/{repo}/actions/runs/{number}/jobs/{jobID}/log`. Artifacts: `WorkflowLogService.ListArtifacts(ctx, runID)`
    and `GET /api/repos/{owner}/{repo}/actions/runs/{number}/artifacts` (with `download_url`), download at
    `GET /{owner}/{repo}/actions/runs/{number}/artifacts/{id}`. The page routes for `/actions/runs/{number}` itself are
    still free.
  - For ticket 06 (secrets): implement `service.SecretSource` (`SecretValues(ctx, repoID)`) and pass it to
    `WorkflowLog.WithSecrets`; set `runner.Executor.Masker` from the Job's secrets so the runner masks too. A secret that
    contains a newline is not masked across the break, because the runner only posts whole lines.
  - Decisions: artifacts are zips (directories and regular files; symlinks dropped), uploaded after the Steps whether or
    not the Job passed and before it finishes (the per-job token dies with the Job); a missing path or a refused upload
    fails an otherwise green Job. The count limit applies per Run attempt. Retention uses the chunk's `created_at` and the
    artifact's `expires_at`, so a changed `ci.log_retention` applies to existing logs but a changed
    `ci.artifact_retention` only to new artifacts. The retention worker runs even with `ci.enabled` off.
  - Reading logs and artifacts is not blocked for an archived repo (archived stays readable); a private or soft-deleted
    repo stops serving at once. The spec line "archived ... stops serving logs" read as run creation, so say if it should
    block reads too.
  - Not run live: SIGTERM of the runner mid-Job (the sandbox refused the `kill`); the stub-docker tests cover the cleanup.
