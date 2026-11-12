# Generate settings, wiki, projects, releases and gists mocks

Created: 2026-11-12
Category: enhancement
Status: wontfix
Blocked by: 01

| Branch                     | Worktree                             | PR title                                                          | Effort |
| -------------------------- | ------------------------------------ | ----------------------------------------------------------------- | ------ |
| `tech/mockgen-repo-extras` | `.worktrees/tech+mockgen-repo-extras` | `tech(mockups): generate settings, wiki, project and release mocks` | L      |

## Problem

See [spec.md](../spec.md). Follows the harness from 01.

## Scope

Register, generate and delete the hand mocks for: `settings` (repo settings; archived and mirror banners, pull-mirror
settings, pending transfer), `wiki` and its five sibling pages (`wiki_edit`, `wiki_new`, and the content pages become
fixtures of one wiki page with a page-missing `--empty` variant), `project`, `project_board`, `releases` (with drafts),
`release_detail`, `release_new`, `gists`, `gist_detail`, `gist_edit`, `gist_new`. Regenerate `index.html` and
`COVERAGE.md`.

## Files

- `cmd/mockgen/**`, `mockups/generated/**`, deleted hand mocks, `mockups/index.html`, `mockups/COVERAGE.md`

## Acceptance criteria

- [ ] Every listed page generated; its hand mock deleted
- [ ] The project-board "deliberate difference" in `COVERAGE.md` is gone because the mock now is the real card behaviour,
      or is restated if the real templ still differs
- [ ] `tests/mockgen-diff` passes for each page
- [ ] Checked at 1440 in light and dark under `make mockups`
- [ ] CI `mockups` job green

## Verification

As in ticket 01.

## Comments
2026-11-12 (maintainer): wontfix. Mocks stay design-first and hand-written, so nothing is generated; see [spec.md](../spec.md). The unmerged branch and PR for this ticket are closed by the maintainer.
