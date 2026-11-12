# Generate pull request mocks

Created: 2026-11-12
Category: enhancement
Status: done
Blocked by: 01

| Branch               | Worktree                       | PR title                                      | Effort |
| -------------------- | ------------------------------ | --------------------------------------------- | ------ |
| `tech/mockgen-pulls` | `.worktrees/tech+mockgen-pulls` | `tech(mockups): generate pull request mocks` | L      |

## Problem

See [spec.md](../spec.md). Follows the harness from 01. `pr.html` is the most state-heavy hand mock.

## Scope

Register, generate and delete the hand mocks for: `pulls` (open, closed, empty), `pr` (open, draft, merged; merge box
`ready`, `needs`, `checks-pending`, `failing`, `changes`, `conflicts`, `auto`; viewers `reviewer`, `author`,
`readonly`; each of the old `#pr=`, `#merge=`, `#viewer=` combinations the templ can reach becomes a named variant),
`pr_commits`, `pr_files` (including diff error states), `pr_checks`, `actions` (the Checks page, with the curl example
when empty), `pull_new`. Regenerate `index.html` and `COVERAGE.md`; the "PR states" bullet in `COVERAGE.md` is replaced
by the generated variant list.

## Files

- `cmd/mockgen/**`, `mockups/generated/**`, deleted hand mocks, `mockups/index.html`, `mockups/COVERAGE.md`

## Acceptance criteria

- [x] Every listed page generated; its hand mock deleted
- [x] Every merge-box state and viewer the old `pr.html` drew has a variant
- [x] `tests/mockgen-diff` passes for each page
- [x] Checked at 1440 in light and dark under `make mockups`
- [ ] CI `mockups` job green

## Verification

As in ticket 01.

## Comments

- `tests/mockgen-diff` reaches 24 of the 50 variants on the seeded server (the rest need states the seed lacks, such as
  required checks or a failed status). Their merge-box titles were checked in the generated HTML instead. Differences
  that follow seeded data sit in `tests/mockgen-diff/allow-sets.json`.
- The CI `mockups` box stays open until the PR's run reports.
