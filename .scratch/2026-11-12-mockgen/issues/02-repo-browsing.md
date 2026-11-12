# Generate repo browsing mocks

Created: 2026-11-12
Category: enhancement
Status: done
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

- [x] Every listed page generated; its hand mock deleted (`raw` stays hand-written, see Comments)
- [x] Variants cover the states `COVERAGE.md` listed for these pages
- [x] `tests/mockgen-diff` passes for each page
- [x] Checked at 1440 in light and dark under `make mockups`
- [ ] CI `mockups` job green (the same `go run ./cmd/mockgen` leaves no diff locally; waits on the PR run)

## Verification

As in ticket 01.

## Comments

- `raw` stays hand-written: `/raw/...` serves plain text, so there is no templ to render. `COVERAGE.md` says so.
- The repository overview leaves the commit heatmap out of the fixture: `HeatmapLarge` is positioned from `time.Now`, so the
  output would change daily.
- The view helpers print whole years off `time.Now` ("1y", "Updated 1y ago"), which would flip the committed files once a
  year. `staticize` now pins that count to `1y`, with a test. Tickets 03–06 get it for free.
- `tests/mockgen-diff` routes gained `resolve` (a URL read off a seeded link) for the commit page. Variants a plain GET
  cannot reach (archived, mirror, empty, binary, conflict, error, no-owner fork) are not diffed; the shared templ is.
