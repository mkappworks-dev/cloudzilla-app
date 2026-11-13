# CI known limitations: runner leftovers, multi-line secrets, admin runners gate, fail-fast

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent
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

- [ ] Runner-created resources carry a runner-id label; start-up and the interval sweep remove only leftovers of this
      runner whose Job it does not hold (stub-`docker` tests assert exact arguments)
- [ ] A secret straddling a chunk boundary is masked in the runner and in the server backstop
- [ ] With `ci.enabled` off the Runners tab is absent and the route returns 404 (router-seam test)
- [ ] A failing matrix Job with `fail-fast` true skips or cancels its siblings and leaves other Jobs alone; `false`
      keeps today's behaviour
- [ ] The four items are removed from "Known limitations" and documented in `docs/ci.md`
- [ ] Checked at desktop 1440 in **light and dark** where the page changes, on a throwaway server (scratch Postgres,
      unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`) with a real `cz-runner` and Docker
- [ ] The standing mock is updated if the page changes
- [ ] `make generate-templ` re-run; `golangci-lint`, `prettier --check` and `go test ./...` pass

## Verification

Throwaway server and real runner: `kill -9` the runner mid-Job, restart, leftovers gone; a failing matrix Job with
`fail-fast` true and false; a multi-line secret through a long log.

## Comments
