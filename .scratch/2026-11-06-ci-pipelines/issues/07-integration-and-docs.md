# Integration: PR checks, commit badges, webhook, API and docs

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent
Blocked by: 06
Parallel with: none

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-18.3-ci-integration` | `.worktrees/feat+phase-18.3-ci-integration` | `feat(ci): checks grouping, status badges, webhook and API (18.3)` | M |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/18-ci-pipelines.md`.

## Problem

Runs work in isolation. The PR Checks tab still shows flat rows, commit lists show no CI state, integrators cannot
hear about a finished Run or script one, and the ROADMAP still describes the abandoned `pipelines` design.

## Scope

- PR Checks tab: built-in Jobs grouped under their Run (`run_id` on the status) with "View run", Approve for a fork
  Run, and the "Other statuses" card for external CI.
- Status icon linking to the latest Run, from the combined commit status, on the commit list, the PR header and the file
  tree's last-commit row, batched so a page of 30 commits adds no per-row queries.
- `workflow_run` webhook event (off by default for existing hooks) fired on Run completion with the payload in the
  spec, HMAC-signed through the existing path; `docs/webhooks.md` updated.
- JSON API under `/api/repos/{owner}/{repo}/actions/`: list runs, get a run, logs by offset, cancel, re-run, dispatch;
  `repo` scope to read and `repo:write` to act; `docs/api-reference.md` and `docs/access-control.md` token-scope table
  updated.
- `docs/ci.md` completed (formats, limits, security model, runner operations, troubleshooting) and linked from
  `CLAUDE.md`'s subsystem list; `docs/ROADMAP.md` rows 18.1–18.3 rewritten to the shipped design (workflow/run
  vocabulary, migrations 136–139) and marked Done; `GLOSSARY.md` checked against the shipped UI text.
- `.scratch/2026-11-06-uiux-desktop-9.5/issues/18-ci-pipelines.md` criteria ticked and `Status: done`; the Run order row
  7 in that folder's `spec.md` updated.

## Files (hotspots in bold)

- `internal/view/pages/pr_checks.templ`, `commits.templ`, `pr_header` templ, `tree` templ, **`internal/view/viewmodels_repo.go`**,
  `viewmodels_pull.go`, `internal/handler/pull_tabs_page_handler.go`, `repo_page_handler.go`
- `internal/service/webhook_service.go`, `internal/service/workflow_run_service.go`, `internal/handler/actions_handler.go`,
  **`internal/router/router.go`**
- `mockups/pr_checks.html`, `commits.html`, `pull.html`, `tree.html`, `COVERAGE.md`
- `docs/ci.md`, `docs/webhooks.md`, `docs/api-reference.md`, `docs/access-control.md`, `docs/ROADMAP.md`, `CLAUDE.md`,
  `GLOSSARY.md`

## Acceptance criteria

- [ ] The PR Checks tab groups Jobs under their Run and still lists external statuses; the Approve button appears only
      for a writer on a waiting fork Run
- [ ] The three badges show the combined state of a commit and link to its latest Run; no extra queries per commit row
      (asserted in a handler test)
- [ ] `workflow_run` fires once on completion with a valid HMAC and is not sent to hooks that did not subscribe
- [ ] The API endpoints enforce scopes and the private-repo 404, and cancel, re-run and dispatch work with a token
- [ ] `docs/ci.md` exists and every config key from tickets 03–05 is in `docs/configuration.md`
- [ ] Checked at desktop 1440 in **light and dark** on a throwaway server (scratch Postgres, unique
      `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`) with a real runner; evidence for both themes in
      the PR
- [ ] The standing mocks in `mockups/` listed under Files are updated in the same PR and checked under `make mockups`
      in both themes
- [ ] `make generate-templ` re-run and generated files committed; `make lint`, `npm run fmt:check` and `go test ./...`
      pass

## Verification

Throwaway server and runner; open a PR with a passing and a failing Job and check the Checks tab, the commit list
badge and the tree row; a local `nc -l` listener receives the `workflow_run` delivery; `curl` the API with a token.
