# CI known limitations: runner leftovers, multi-line secrets, admin runners gate, fail-fast

Created: 2026-11-13
Category: enhancement
Status: done
Blocked by: none
Parallel with: none

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `fix/phase-18-ci-known-limits` | `.worktrees/fix+phase-18-ci-known-limits` | `fix(ci): runner leftovers, multi-line secrets, admin runners gate and fail-fast (18.x)` | M |

Spec: [spec.md](../spec.md). Follows [07](07-integration-and-docs.md).

## Problem

Four limitations are listed under "Known limitations" in `docs/ci.md`: a `kill -9` of a runner leaves `cz-job-*`
containers, networks and volumes behind; a multi-line secret split across two log chunks is not masked; `/admin/runners`
and its tab show while `ci.enabled` is off; `strategy.fail-fast` is parsed but ignored.

## Scope

1. Runner labels every container, network and volume with its id; on `cz-runner run` start and on a slow interval it
   removes labelled leftovers whose Job it no longer holds.
2. `logmask` masks each non-empty line (4 characters or more) of a multi-line secret, with its base64 and URL-encoded
   forms, in the runner and in the server backstop.
3. The admin tab strip hides Runners, and `/admin/runners` plus the admin side of runner management return 404, while
   `ci.enabled` is off.
4. The scheduler honours `strategy.fail-fast` (default true): a failed matrix Job skips its not-started siblings and
   cancels its running ones; `fail-fast: false` is unchanged.

## Files

- `internal/ci/runner/*`, `cmd/cz-runner/main.go`
- `internal/ci/logmask/logmask.go`, `internal/service/workflow_log_service.go`
- `internal/view/pages/admin_nav.templ`, admin base view-model, `internal/router/router.go`, admin runners handler
- `internal/service/workflow_run_schedule.go`
- `docs/ci.md`; `mockups/admin_runners.html` and `COVERAGE.md` only if the page changes

## Acceptance criteria

- [x] Runner-created resources carry a runner-id label; start-up and the interval sweep remove only leftovers of this
      runner whose Job it does not hold (stub-`docker` tests assert exact arguments)
- [x] A secret straddling a chunk boundary is masked in the runner and in the server backstop
- [x] With `ci.enabled` off the Runners tab is absent and the route returns 404 (router-seam test)
- [x] A failing matrix Job with `fail-fast` true skips or cancels its siblings and leaves other Jobs alone; `false`
      keeps today's behaviour
- [x] The four items are removed from "Known limitations" and documented in `docs/ci.md`
- [x] Checked at desktop 1440 in **light and dark** where the page changes, on a throwaway server (scratch Postgres,
      unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`) with a real `cz-runner` and Docker
- [x] The standing mock is updated if the page changes
- [x] `make generate-templ` re-run; `golangci-lint`, `prettier --check` and `go test ./...` pass

## Verification

Throwaway server and real runner: `kill -9` the runner mid-Job, restart, leftovers gone; a failing matrix Job with
`fail-fast` true and false; a multi-line secret through a long log.

## Comments

- Claude, 2026-11-13: done. Evidence (throwaway server, two real runners and Docker; `kill -9` leftovers removed on restart;
  fail-fast true and false; a multi-line secret split across two chunks) is in `/private/tmp/claude-501/ci-ticket-08-evidence.md`.
  - Migration 140 adds `workflow_jobs.fail_fast` (default true), copied from the matrix strategy, so the scheduler needs no
    workflow file. A failed-only re-run also runs the siblings fail-fast skipped.
  - Runner: `Config.RunnerID` is written by `register`; an older config labels with a digest of its token. Resources of
    an older runner carry no label and are removed by hand. Sweep runs at start and every 10 minutes.
  - Multi-line secrets: lines under 4 characters are not masked alone, documented in `docs/ci.md`.
  - Admin: the "CI is switched off" banner on `/admin/runners` is gone, since the page is a 404 in that state. Browser
    screenshots could not be saved in this worktree (the tool's allowed roots are another worktree), so the tab strip was asserted
    from the DOM at 1440 in light and dark, and with curl; the page only loses one tab, so `mockups/` is unchanged.
  - Noticed: a parallel `go test ./internal/handler` run hit a DB deadlock once and passed on rerun.
