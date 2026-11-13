# Actions UI: runs list, run page, secrets and CI/CD settings

Created: 2026-11-13
Category: enhancement
Status: done
Blocked by: 01, 05
Parallel with: none

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-18.3-actions-ui` | `.worktrees/feat+phase-18.3-actions-ui` | `feat(ci): actions tab, run page and CI settings (18.3)` | L |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/18-ci-pipelines.md`.

## Problem

Runs, Jobs, logs and artifacts exist only in the database. Users need the Actions tab the ticket decided, the run page
with live logs, the controls (dispatch, cancel, re-run, approve) and the secrets and CI/CD settings the Workflows need.

## Scope

- Migration (next free number; `139_create_ci_secrets.sql` as of the spec): repo secrets (name, sealed value, creator,
  timestamps). Sealed with `secretbox` under a `ci-secret` purpose; refused when no `security.secret_key`. Secrets are
  delivered to the claim response for non-fork Runs only, and the log masker (ticket 05's hook) masks exact, base64
  and URL-encoded forms.
- Routes and pages `GET /{owner}/{repo}/actions` (runs list: workflow, branch, status, trigger filters; "Commit
  statuses" filter shows external CI; pagination; empty states per the spec) and `GET /{owner}/{repo}/actions/runs/{n}`
  (stage graph, steps as collapsible sections, line anchors, timestamps toggle, search, download, artifacts, queued and
  no-runner states, YAML-error state). While a Job runs, the page polls the tail by line offset every 2 s with
  `hx-trigger` and stops when it finishes.
- Controls: Run workflow popover (workflow, ref select), Cancel (confirm dialog), Re-run all / failed, Approve for a
  fork Run, all `CanWrite` and POST with CSRF. Reader-only users see no controls.
- Tab: the repo sub-nav's Checks tab becomes Actions (hidden when `ci.enabled` is off or the repo has CI off and no
  Runs). `/{owner}/{repo}/checks` redirects to `/actions` (preserving external statuses under the filter).
- Settings → CI/CD section: enable toggle, secrets (add/replace/delete, write-only values), the repo runners block from
  ticket 04, and the supported-format reference link.
- `RunStatusIcon` shared component with the new statuses; `checkRowIcon` reuses it.
- Reserved route check: `actions` cannot clash with an owner or repo name.

## Files (hotspots in bold)

- `internal/db/migrations/139_create_ci_secrets.sql` (new), `internal/model/ci_secret.go` (new),
  `internal/store/ci_secret_store.go` (new), `internal/service/ci_secret_service.go` (new)
- **`internal/handler/actions_page_handler.go`** (new), `internal/handler/actions_handler.go` (new),
  `internal/handler/checks_page_handler.go`, **`internal/router/router.go`**, `internal/middleware/setup.go` (check)
- `internal/view/pages/actions.templ`, `actions_run.templ`, `repo_settings.templ` (all new or extended),
  **`internal/view/viewmodels_repo.go`**, `internal/view/components/run_status_icon.templ` (new),
  **`internal/view/fragments/repo_subnav.templ`**, `internal/view/pages/checks.templ`
- `mockups/actions.html`, `actions_run.html`, `repo_settings_ci.html`, `COVERAGE.md`, `index.html`, `shell.js`
- `tailwind/input.css` only if a class is missing

## Acceptance criteria

- [x] Every state in the mock renders from real data: runs, empty, no-match, YAML error, queued, no-runner, running,
      failed, success, cancelled, runner-lost, waiting for approval
- [x] A running Job's log tails every 2 s without reloading the page and stops when the Job ends; anchors, timestamps
      toggle, search and download work
- [x] Dispatch, Cancel, Re-run (all / failed) and Approve work for a writer and are absent and refused (403/404) for a
      reader and a signed-out user; a private repo shows the same 404 to a non-reader as a missing repo
- [x] A secret saved in the UI is never rendered back, reaches a non-fork Job's env, is masked in the log, and is not
      delivered to a fork Run; saving with no `security.secret_key` is refused with a message
- [x] `/checks` redirects to `/actions`; the "Commit statuses" filter lists external CI as the old page did
- [x] No `x-show`/`space-y` pitfalls from CLAUDE.md (rendered element last); `make generate-templ` re-run and the
      `_templ.go` files committed
- [x] Checked at desktop 1440 in **light and dark** on a throwaway server (scratch Postgres, unique
      `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`) with a real runner; evidence for both themes in
      the PR
- [x] The standing mocks in `mockups/` listed under Files are updated in the same PR and checked under `make mockups`
      in both themes; the mocks match the shipped pages
- [x] `make lint`, `npm run fmt:check` and `go test ./...` pass

## Verification

Throwaway server and runner; push a three-Job workflow with a matrix, a failing Step and an artifact; walk the list,
run page (live log while running), cancel, re-run failed, a fork run's approval and the secrets page, in light and dark
at 1440 with the built-in browser (a hidden pane stalls Alpine transitions: assert state, then screenshot).

## Comments

- Claude, 2026-11-13: done, migration 139 (`ci_secrets`). Commits: secrets (`01a84daa`), then the Actions tab, run page and
  CI/CD settings. Evidence (real runner and Docker on a throwaway server, both themes at 1440) is in
  `/private/tmp/claude-501/ci-ticket-06-evidence.md`; screenshots are under `.scratch/2026-11-06-ci-pipelines/shots/t06-*`
  (gitignored): copy them into the PR.
  - Secrets reach the runner as `JobSpec.Secrets` (empty for a fork Run, i.e. `workflow_runs.no_secrets`). The runner
    resolves `${{ secrets.X }}` itself (`workflow.Context.Secrets`; nil on the server, so the parse-time rule is unchanged),
    builds its masker from them, and the server's `WorkflowLog.WithSecrets` masks again.
  - Mock vs shipped: the log stores no per-line times, so the Timestamps toggle shows each Step's start clock in its
    header (mock changed to match); log line numbers run through the Job and anchor as `#L<n>`; the graph header has no
    `on:` text; the dispatch hint says `workflow_dispatch`. The shared running icon is `text-foreground` (the mock's run
    page used `text-warning`; fixed).
  - `/checks` redirects (301, query kept) when `ci.enabled` is on; with it off the Actions routes don't exist, so
    `/checks` keeps listing commit statuses and the tab is hidden. The tab also hides when the repo's CI is off and it has
    no Runs.
  - A refused control redirects back with `?err=<code>`; the page shows a fixed sentence per code.
  - Live tail: `hx-trigger="every 2s"` on a hidden poller; each reply swaps in the next poller and appends lines to their
    Step by out-of-band swap; when the Job ends it answers `HX-Refresh`.
  - For ticket 07: `components.RunStatusIcon`/`RunStatusIconSized` (accept commit-status states too), `checksCommitCard` now
    lives in `actions.templ`, `WorkflowRunService.ListRunsBySHA` and the store's `ActionsVisible` are available; the run
    URL is `/{owner}/{repo}/actions/runs/{n}`; `h.withRepoSubnav` sets `ShowActions`.
  - Not done / noticed: a fork Run could not be shown live (no cross-repo pull requests yet, see ticket 03), so approval
    is covered by the router test only. The runner's claim long poll (25 s) outlives the server's 15 s write timeout and
    logs `EOF` then retries (seen in the runner log; from ticket 04). A matrix expression in an `artifacts:` name is not
    interpolated (`dist-----matrix.arch---`).
