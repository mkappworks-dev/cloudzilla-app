# Generate repo browsing mocks

Created: 2026-11-12
Category: enhancement
Status: wontfix
Blocked by: 01

| Branch              | Worktree                      | PR title                                       | Effort |
| ------------------- | ----------------------------- | ---------------------------------------------- | ------ |
| `tech/mockgen-repo` | `.worktrees/tech+mockgen-repo` | `tech(mockups): generate repo browsing mocks` | M      |

## Problem

See [spec.md](../spec.md). Follows the harness from 01.

## Scope

Register fixtures and variants, generate, and delete the hand mocks for: `repo`, `tree`, `blob` (text and binary),
`blame`, `raw`, `edit_file`, `new_file`, `commits`, `commit`, `contributors`, `pulse`, `activity`, `code_search`,
`search`, `refs`, `dependencies`, `stargazers`, `topic`, `repo_fork`. Add `--empty` (repo with no commits, empty
directory, no results) and read-only/anon variants where the templ branches. Update `index.html` and `COVERAGE.md` by
regeneration.

## Files

- `cmd/mockgen/**`, `mockups/generated/**`, deleted hand mocks, `mockups/index.html`, `mockups/COVERAGE.md`

## Acceptance criteria

- [ ] Every listed page generated; its hand mock deleted
- [ ] Variants cover the states `COVERAGE.md` listed for these pages
- [ ] `tests/mockgen-diff` passes for each page
- [ ] Checked at 1440 in light and dark under `make mockups`
- [ ] CI `mockups` job green

## Verification

As in ticket 01.

## Comments
2026-11-12 (maintainer): wontfix. Mocks stay design-first and hand-written, so nothing is generated; see [spec.md](../spec.md). The unmerged branch and PR for this ticket are closed by the maintainer.
