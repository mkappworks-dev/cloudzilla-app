# Runner protocol, `cz-runner` and runner admin pages

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent
Blocked by: 03
Parallel with: 01

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-18.1-cz-runner` | `.worktrees/feat+phase-18.1-cz-runner` | `feat(ci): cz-runner and runner protocol (18.1)` | L |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/18-ci-pipelines.md`. ADR 0003.

## Problem

Queued Jobs have nothing to run them. This ticket is the security boundary: the protocol a Runner speaks, the per-job
token, and a runner binary that starts only hardened containers.

## Scope

- Migration (next free number; `137_create_ci_runners.sql` as of the spec): runners (scope Instance or Repository, name,
  labels, hashed token, last contact, revoked), registration tokens (hashed, one-time, scope), and the Job lease and
  heartbeat columns if ticket 03 did not add them.
- Runner API under `/api/runner/`: `register` (one-time token for a long-lived runner token), `claim` (long-poll with
  labels, returns one Job, the resolved Steps, a per-job token and the non-secret env), `heartbeat`, `step` status and
  `finish`. Runner tokens are stored hashed; revoking takes effect on the next request. A Job with no heartbeat for 60 s
  fails as "runner lost" (service method plus sweep).
- Per-job token: read-only, bound to one repo and one Job, valid for git clone over HTTP and for that Job's log and
  artifact endpoints, and nothing else; expires when the Job ends. The git HTTP auth path accepts it for upload-pack
  only; a test proves it cannot push or reach another repo.
- `cmd/cz-runner`: `register --server --token --labels`, `run`. It claims, clones the SHA into a workspace volume, then
  runs each Step in a container by shelling out to `docker`: not privileged, no host mounts, no Docker socket,
  `cap-drop=ALL`, `no-new-privileges`, non-root, CPU, memory and PID limits (flags, defaults 2 / 4 GiB / 512), a
  per-job network, workspace removed afterwards. Label-to-image map and `allowed_images` in its config. Step `env`,
  `working-directory`, `shell`, `timeout-minutes` and the Job timeout are honoured; secrets arrive as env and are masked
  by ticket 05's log path (this ticket passes none).
- Runner management: `/admin/runners` (superadmin) and a repo CI/CD runners section (CanManage), as drawn in ticket 01.
  Registration-token creation, runner list, revoke. Minimal templ pages; the full CI/CD settings page is ticket 06.
- Dev-only compose profile running a runner against the host Docker socket; `docs/ci.md` runner section and operator
  hardening notes (egress ranges to block).

## Files (hotspots in bold)

- `internal/db/migrations/137_create_ci_runners.sql` (new), `internal/model/ci_runner.go` (new)
- **`internal/store/stores.go`**, `internal/store/ci_runner_store.go` (new), **`internal/service/services.go`**,
  `internal/service/ci_runner_service.go` (new), `internal/service/workflow_run_service.go`
- `internal/handler/ci_runner_handler.go` (new), **`internal/router/router.go`**, `internal/handler/git_http_handler.go`
  (token acceptance), `cmd/server/main.go` (sweep worker)
- `cmd/cz-runner/` (new), `Makefile`, `docker-compose*.yml`, `internal/view/pages/admin_runners.templ` (new),
  `internal/view/pages/repo_settings.templ`, `internal/view/viewmodels_*.go`
- `mockups/admin_runners.html`, `docs/ci.md`, `docs/configuration.md`

## Acceptance criteria

- [ ] Register, claim, heartbeat, step and finish work end to end against the router with a fake runner; a revoked
      runner token is refused at once; an expired or foreign per-job token is refused
- [ ] A per-job token clones its repo but cannot push, clone another repo, or call a user API
- [ ] A Job with no heartbeat for 60 s becomes `failed` "runner lost" and its Run fails
- [ ] With a stub `docker` on `PATH`, every started container's argument list contains the hardening flags, none of
      them can be changed by a workflow key, and an image outside `allowed_images` fails the Job with the reason
- [ ] A runner behind no inbound port works (polling only); the server never dials a runner
- [ ] A Job whose label matches no runner stays `queued`
- [ ] Runner pages are checked at desktop 1440 in **light and dark**, evidence for both in the PR
- [ ] The standing mock `mockups/admin_runners.html` (and the repo runners mock from ticket 01) matches the shipped
      pages and is checked under `make mockups` in both themes
- [ ] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` /
      `cloudzilla-seed`) with a real `cz-runner` and real Docker: push a workflow, watch the Job run to success and to
      failure, kill the runner and see "runner lost"
- [ ] `make lint` and `go test ./...` pass

## Verification

Throwaway server and a runner registered from `/admin/runners`; `docker ps` while a Job runs shows the hardened flags
(`docker inspect`); a Step that tries `--privileged`-style escapes (mounting `/`, reading `/proc/1/environ` of the host)
fails inside the container.
