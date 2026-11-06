# PR list filters, chips, menus and pagination

Created: 2026-11-06
Category: enhancement
Status: ready-for-agent
Blocked by: 01, 02

| Branch | Worktree | PR title |
| --- | --- | --- |
| `feat/pr-list-filters` | `.worktrees/feat+pr-list-filters` | `feat(pulls): qualifier search, filter menus and pagination` |

Spec: [../spec.md](../spec.md).

## Scope

- `PullStore`: filtered, sorted, paged list with `(rows, total)` in SQL (text, author, assignee, label AND, milestone,
  `no:`, `is:open|draft|merged|closed` as SQL predicates, sort); unfiltered per-state counts replacing the Go bucketing.
- `PullService.ListWithCIStatus` takes real `limit, offset`; CI status runs only for the page.
- `PagePulls`: parse `q` plus the `state=` alias, page size 25, clamp `page`; reuse the shared filter components from
  ticket 02 (no Review menu or review-state indicator; both wait for the follow-up after ticket 09).
- Placeholder `Search or filter, e.g. is:open label:bug author:@me`; filter-aware empty state and repo-empty state.
- Standing mocks: `mockups/pulls.html` (and any other changed page); `COVERAGE.md`.

## Acceptance criteria

- [ ] Every qualifier in the spec filters PRs correctly; `is:draft` / `is:merged` match the old tabs
- [ ] Filtering, sorting and paging run in SQL; tab counts stay unfiltered; CI resolves for at most 25 rows per page
- [ ] Works with JavaScript disabled; back/forward and shared URLs reproduce the view
- [ ] Standing mocks updated and checked under `make mockups`; verified at 1440 in light and dark on a throwaway server
- [ ] Benchmark shows before/after for the list query
- [ ] Store and handler tests cover the filters; `/code-review` run
- [ ] On merge, parent ticket 10 set to `done` and its criteria ticked

## Verification

Throwaway server per the parent spec's harness.
