# Rework the CI mock: Actions tab and standing mocks

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent
Blocked by: none
Parallel with: 02, 03

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-18-actions-mock` | `.worktrees/feat+phase-18-actions-mock` | `feat(ci): actions tab mock (18.3)` | M |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/18-ci-pipelines.md`.

## Problem

The scratch mock puts built-in runs inside the Checks tab and assumes a single naming ("pipelines"). The decisions are
now: an **Actions** tab at `/actions` and `/actions/runs/{n}`, the PR Checks tab as the shared results view, "run" and
"workflow" as the vocabulary, fork-approval, runner and secrets surfaces. Every later UI ticket copies classes from this
mock, so it has to be right first.

## Scope

- Rename and rework `mock.html` as the Actions runs list: tab bar highlights **Actions**; "Commit statuses" becomes a
  filter (external CI) beside workflow, branch, status and trigger; "Pipelines" wording removed everywhere.
- New statuses drawn once with a shared icon set: `queued` (muted clock), `waiting` ("Approval required", with an
  Approve button), `running` (pulsing dot), `success`, `failure`, `cancelled` (slashed circle), `skipped` (dash).
- `run.html`: add the fork-approval banner state, a `no-runner` state ("No runner with label `gpu`" on the queued Job),
  a `runner-lost` failed Job state, the secrets-masked log line (`***`), artifacts with expiry, and the "log truncated"
  line. Keep the existing states.
- `pr_checks.html`: groups built-in Jobs under their Run with "View run", plus the Approve button for a fork run.
- New mock pages: repo Settings → CI/CD (enable toggle, secrets list with add/replace/delete and write-only values,
  repo runners with registration-token flow and a copyable `cz-runner register` command) and `/admin/runners` (instance
  runners, labels, last contact, revoke).
- Small mocks of the three status icons for the commit list, PR header and file-tree last-commit row.
- Standing mocks in `mockups/`: rename `actions.html` content from the Checks page to the Actions list (the file already
  exists and maps to the Checks page today), add run, settings and admin runner pages, and update `COVERAGE.md`,
  `index.html` and `shell.js` where affected. The scratch mock's README is rewritten to the final decisions and
  its old "ROADMAP vs mock" table removed.

## Files (hotspots in bold)

- `.scratch/2026-11-06-ci-pipelines/mock.html`, `run.html`, `pr_checks.html`, `mock-input.css`, `README.md`, new
  `settings.html`, `admin_runners.html`
- **`mockups/actions.html`**, `mockups/pr_checks.html`, new `mockups/actions_run.html`, `mockups/repo_settings_ci.html`,
  `mockups/admin_runners.html`, **`mockups/COVERAGE.md`**, `mockups/index.html`, `mockups/shell.js`, `mockups/commits.html`,
  `mockups/pull.html` (status icons only)

## Acceptance criteria

- [ ] Every state listed above renders from `?state=` and has no "Pipelines" wording
- [ ] The tab bar highlights Actions on the list and run pages and Pull requests on the PR Checks page, matching the real
      app's behaviour
- [ ] Classes come from existing templ components (table, tabs, accordion, alert, dialog, dropdown menu, blob line
      table, release asset rows); no class absent from `tailwind/input.css`'s sources
- [ ] Checked at desktop 1440 in **light and dark** under `make mockups`; screenshots of both themes in the PR
- [ ] The standing mocks in `mockups/` listed under Files are updated in the same PR and `COVERAGE.md` / `index.html`
      reflect them
- [ ] ~~Verified on a throwaway server~~ (static mocks only; no server code in this ticket)
- [ ] `npm run fmt:check` passes for the docs touched; `.scratch/` is not formatted

## Verification

`make mockups`, open each page and each `?state=` in light and dark at 1440; compare with `internal/view/pages/checks.templ`
and `pr_checks.templ` for the class sources.
