# Rename UI and mock

Created: 2026-11-12
Category: enhancement
Status: ready-for-agent
Blocked by: 02

| Branch                | Worktree                         | PR title                                       | Effort |
| --------------------- | -------------------------------- | ---------------------------------------------- | ------ |
| `feat/repo-rename-ui` | `.worktrees/feat+repo-rename-ui` | `feat(ui): rename a repository from settings` | S      |

## Problem

See [spec.md](../spec.md). The settings name field is read-only with "Renaming from settings is not yet available".

## Scope

- `repo_settings.templ`: the name input becomes editable with its own **Rename** button inside the General card, a
  separate form from the General save. It opens the existing danger-confirm dialog (`data-confirm-danger`), naming the
  new URL and saying old links and remotes keep redirecting. Show only to `CanManage` viewers.
- `POST /{owner}/{repo}/settings/rename` (`PageRepoSettings` neighbours; JSON/form handler in `repo_handler.go`), with
  inline errors for taken, invalid, reserved and "a mirror is syncing"; `HX-Redirect` to the new settings URL on success.
  Route in `router.go`; add it to the setup-path check only if it must work before setup completes (it must not).
- Mock `mockups/settings.html` (and `COVERAGE.md` if affected): the rename form, dialog, and an error state. Copy classes
  from the templ; follow `.scratch/2026-11-06-uiux-desktop-9.5/spec.md` rules for UI tickets.
- Run `make generate-templ`; commit the regenerated `*_templ.go`.

## Files

- `internal/view/pages/repo_settings.templ` (+ generated), `internal/view/viewmodels_*.go`, `internal/handler/repo_handler.go`,
  `internal/router/router.go`, `mockups/settings.html`, `mockups/COVERAGE.md`

## Acceptance criteria

- [ ] The real page and the mock show the same form, dialog and error state
- [ ] Checked at 1440 in light and dark, real page and mock under `make mockups`, evidence for both in the PR
- [ ] A viewer without `CanManage` sees no Rename control
- [ ] Success redirects to `/{owner}/{newname}/settings`; the old settings URL then redirects there too (ticket 04)
- [ ] `make lint`, `npm run fmt:check` pass

## Verification

Throwaway server and Playwright script (cached `playwright-core`): open settings, rename, assert the URL and header;
screenshot the dialog and error in both themes. Don't use the shared dev DB.

## Comments
