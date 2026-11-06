# Issue list filters, chips, menus and pagination

Created: 2026-11-06
Category: enhancement
Status: ready-for-agent
Blocked by: 01

| Branch | Worktree | PR title |
| --- | --- | --- |
| `feat/issue-pr-list-filters` | `.worktrees/feat+issue-pr-list-filters` | same PR as 01 and 03 |

Spec: [../spec.md](../spec.md).

## Scope

- Migration: the indexes listed in the spec (next free number after 117, re-checked after merging main).
- `IssueStore`: filtered, sorted, paged list with `(rows, total)` in SQL (label AND, milestone by title, author,
  assignee, `no:`, text, visibility); an unfiltered open/closed count query replacing the Go loops. Remove the 500 cap
  from this path.
- `IssueService` builds the filter from `listquery.Query`, resolving `@me` and milestone titles.
- `PageIssues`: parse `q` plus aliases, page size 25, clamp `page`, pinned issues only on the default page 1, pass
  priority and comment count (`CountByIssueIDs`) to `IssueListRow`.
- Shared components in `internal/view/components/`: filter bar with chips (× links), `<details>` menus (multi-select
  labels, single-select others) with search box and Apply, "N results · Clear filters", filter-aware empty states.
  Pagination links carry `q`.
- Search placeholder: `Search or filter, e.g. is:open label:bug author:@me`.
- Standing mocks: `mockups/issues.html`, `issues_closed.html` (and any other page whose markup changes); `COVERAGE.md`.

## Acceptance criteria

- [ ] Every qualifier in the spec filters correctly; `no:` and `@me` work; legacy params still resolve
- [ ] Filtering, sorting and paging run in SQL; tab counts stay unfiltered; out-of-range `page` clamps
- [ ] Works with JavaScript disabled; back/forward and shared URLs reproduce the view
- [ ] Rows show priority and comment count
- [ ] Standing mocks updated and checked under `make mockups`; verified at 1440 in light and dark on a throwaway server
- [ ] Large-repo benchmark shows before/after for the list query
- [ ] Store and handler tests cover the filters; `/code-review` run

## Verification

Throwaway server per the parent spec's harness; `ada-silva/hollow-atlas` for a busy repo.
