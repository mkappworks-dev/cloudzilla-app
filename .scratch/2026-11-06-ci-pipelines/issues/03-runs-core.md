# Runs core: schema, run creation, statuses, fork approval

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent
Blocked by: 02
Parallel with: 01

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-18.1-runs-core` | `.worktrees/feat+phase-18.1-runs-core` | `feat(ci): workflow runs, jobs and statuses (18.1)` | L |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/18-ci-pipelines.md`. ADR 0003.

## Problem

A push creates nothing and the commit status table cannot say which Run a status belongs to. Runners (ticket 04) need
Runs and Jobs to claim, and the UI needs rows to list.

## Scope

- Migration (next free number; `136_create_workflow_runs.sql` as of 4027d560): `workflow_runs`, `workflow_jobs`,
  `workflow_steps`, the per-repo run number, `commit_statuses.run_id` (nullable, `ON DELETE SET NULL`), a repo-level CI
  enabled column (default true), and the stored workflow file contents on the Run. Statuses as in the spec.
- Models, stores wired into `Stores`, `WorkflowRunService` wired into `Services`.
- Run creation: from `PushService.AfterPush` (push and tag events; a delete-only push creates none), from PR
  open/reopen/update, and from a `Dispatch` service method. It reads `.cloudzilla/workflows/` at the commit, parses with
  ticket 02's package, applies trigger matching, expands the matrix into Jobs, and writes everything in one
  transaction. A parse error creates a failed Run with zero Jobs and one failing status `ci/<workflow file stem>`.
- Statuses: each Job upserts `ci/<job id>` into `commit_statuses` with `run_id`, creator = triggering user or the repo
  owner for a deploy-key push. Branch protection's required checks need no change; a test proves a required `ci/test`
  blocks a merge until the Job succeeds.
- Concurrency: `concurrency.group` with `cancel-in-progress`, the default new-push-cancels-queued rule, the
  queued-run cap (oldest `queued` cancelled), and the 24 h "no runner" cancel as a sweep method.
- Service methods for cancel, re-run (all or failed Jobs, same stored file and SHA, attempt+1) and the `needs` / `if`
  scheduler that moves Jobs `queued` once their needs finish and marks `skipped` ones.
- Fork pull requests: a PR whose head repo differs creates a `waiting` Run; `Approve` (CanWrite) queues it; a new push
  makes a new waiting Run. Fork and approved fork Runs are flagged `no_secrets` for later tickets.
- Config: `ci.enabled`, `ci.max_jobs_per_run`, `ci.max_steps_per_run`, `ci.max_queued_runs_per_repo`,
  `ci.max_concurrent_runs_per_repo`, `ci.max_job_minutes` in `docs/configuration.md`. Nothing runs without a Runner, so
  Jobs stay `queued`.
- `docs/git-transport.md` "After a push" gains the Run-creation step.

## Files (hotspots in bold)

- `internal/db/migrations/136_create_workflow_runs.sql` (new), `internal/model/workflow_run.go` (new)
- **`internal/store/stores.go`**, `internal/store/workflow_run_store.go` (new), `internal/store/commit_status_store.go`
- **`internal/service/services.go`**, `internal/service/workflow_run_service.go` (new), **`internal/service/push_service.go`**,
  `internal/service/pull_service.go`, `internal/service/commit_status_service.go`
- `internal/config/config.go`, `docs/configuration.md`, `docs/git-transport.md`

## Acceptance criteria

- [ ] A native `git push` of a commit with a valid workflow creates a Run with its Jobs and `ci/<job>` pending
      statuses; a delete-only push creates none; a workflow with a YAML error creates a failed Run and one failing
      status
- [ ] A matrix expands to the expected Jobs; a Job with unmet `needs` stays blocked and moves `queued` when they finish
      (driven by service calls in the test)
- [ ] A required status check `ci/test` blocks the PR merge until the Job succeeds, and a failing workflow status
      blocks it too
- [ ] A fork PR creates a `waiting` Run; Approve queues it; a second push creates a second waiting Run; a reader's
      Approve is refused
- [ ] Cancel and re-run (all, failed only) behave per the spec and keep the old attempt's rows
- [ ] `cancel-in-progress` and the queued-run cap cancel the right Runs; the 24 h sweep cancels an unclaimed Job
- [ ] A repo with CI switched off, an archived repo and a soft-deleted repo create no Runs
- [ ] ~~Light and dark checks at 1440~~ (no UI; the UI is ticket 06)
- [ ] ~~Standing mocks in `mockups/` updated~~ (no UI)
- [ ] Verified on a throwaway server: scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` /
      `cloudzilla-seed`; a native `git push` over HTTP and SSH creates the Run (read from the database), evidence in
      the PR
- [ ] `make lint` and `go test ./...` pass (router-seam tests use the push client)

## Verification

Throwaway server per `docs/testing.md`; push `.cloudzilla/workflows/ci.yml` with native git, then `psql` the run, job
and status rows. Repeat from a fork repo for the `waiting` state.
