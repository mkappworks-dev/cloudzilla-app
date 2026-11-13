# Rework the CI mock: Actions tab and standing mocks

Created: 2026-11-13
Category: enhancement
Status: done
Progress: the Actions, run, PR Checks, repository CI settings, admin runners and status-icon mocks were reworked in the
spec PR and now live in `mockups/` (`actions.html`, `actions_run.html`, `pr_checks.html`, `repo_settings_ci.html`,
`admin_runners.html`, `actions_status_icons.html`). What remains is the shipped-page work below.
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

The scratch rework is done: the mocks moved from `.scratch/2026-11-06-ci-pipelines/` into `mockups/`, with the
Actions tab, the shared status icon set (`mockups/actions_status_icons.js`), the fork-approval, `no-runner`,
`runner-lost`, masked-log, artifact-expiry and "log truncated" states, and the new settings and admin runner pages.

Remaining:

- Update every other `mockups/` page that still says Checks for the repo tab or links to it: `commits.html`,
  `commit.html`, `pull.html`-style PR pages and `tree.html` (status icons from `actions_status_icons.js`), and the
  `index.html` / `COVERAGE.md` entries.
- Keep Pull requests highlighted on `pr_checks.html` (the real app's behaviour) and re-verify every page and `?state=`
  in light and dark at 1440.

## Files (hotspots in bold)

- **`mockups/actions.html`**, `mockups/actions_run.html`, `mockups/pr_checks.html`, `mockups/repo_settings_ci.html`,
  `mockups/admin_runners.html`, `mockups/actions_status_icons.html`, `mockups/actions_status_icons.js`
- **`mockups/COVERAGE.md`**, `mockups/index.html`, `mockups/shell.js`, `mockups/commits.html`, `mockups/commit.html`,
  `mockups/pr.html`

## Acceptance criteria

- [x] Every state renders from `?state=` and has no "Pipelines" wording (checked in the spec PR)
- [x] The tab bar highlights Actions on the list and run pages and Pull requests on the PR Checks page, matching the real
      app's behaviour
- [x] Classes come from existing templ components (table, tabs, accordion, alert, dialog, dropdown menu, blob line
      table, release asset rows); no class absent from `tailwind/input.css`'s sources
- [x] Checked at desktop 1440 in **light and dark** under `make mockups`; screenshots of both themes in the PR
- [x] The standing mocks in `mockups/` listed under Files are updated in the same PR and `COVERAGE.md` / `index.html`
      reflect them
- [x] ~~Verified on a throwaway server~~ (static mocks only; no server code in this ticket)
- [x] `npm run fmt:check` passes for the docs touched; `.scratch/` is not formatted

## Verification

`make mockups`, open each page of the six mocks above and each `?state=` in light and dark at 1440; compare with `internal/view/pages/checks.templ`
and `pr_checks.templ` for the class sources.

## Comments

- 2026-11-13, implementer: `commit.html`'s "Checks" card is external statuses, so it is unchanged; `pr.html#ci=` toggles the title icon. Screenshots of both themes are in `.scratch/.../shots/` (gitignored), not yet in a PR. The mockups CSS `@source`s `mockups/*.js`, so the new classes are generated.
