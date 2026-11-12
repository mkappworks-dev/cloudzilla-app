# Generate issues, milestones and discussions mocks

Created: 2026-11-12
Category: enhancement
Status: ready-for-agent
Blocked by: 01

| Branch                | Worktree                        | PR title                                                        | Effort |
| --------------------- | ------------------------------- | --------------------------------------------------------------- | ------ |
| `tech/mockgen-issues` | `.worktrees/tech+mockgen-issues` | `tech(mockups): generate issue, milestone and discussion mocks` | M      |

## Problem

See [spec.md](../spec.md). Follows the harness from 01.

## Scope

Register, generate and delete the hand mocks for: `issues` (open, closed, filtered with chips and a menu open, empty, the
unrecognized-qualifier notice), `issue_detail` (open, closed, locked, read-only, signed-out), `issue_new`, `milestones`,
`milestone_detail`, `milestone_new`, `discussions` (all, answered, closed, empty, `?category=`), `discussion_detail`
(plain, locked), `discussion_new`. Hash and query toggles in the old mocks become variants. Regenerate `index.html` and
`COVERAGE.md`.

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
