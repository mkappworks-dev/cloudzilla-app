# Issue and PR list filters and pagination

Created: 2026-11-06
Category: enhancement
Status: ready-for-agent

Mock: [README.md](./README.md), [mock.html](./mock.html). Parent ticket:
`../2026-11-06-uiux-desktop-9.5/issues/10-issue-pr-list-filters.md`.

## Problem

`PageIssues` loads every issue (silently capped at 500), filters and sorts in Go, and ignores the qualifiers its search
box once advertised. `PagePulls` reads only `state` and loads every PR. Neither list has search (PRs), author or
assignee filters, sorting (PRs) or pagination.

## Decisions (grilled with the maintainer 2026-11-07)

- **State lives in `q`.** One string, e.g. `is:open label:bug label:ui author:priya sort:updated fix crash`, plus
  `page=N`. Tabs, menus and chips all write `q`. The old `state=`, `label=`, `milestone=` and `sort=` params keep working
  as aliases (read, then folded into `q`).
- **Qualifiers**, shared by both lists: `is:open|closed` (`is:draft|merged` on PRs), `author:`, `assignee:`, `label:`
  (repeatable, AND), `milestone:` (by title), `no:label|milestone|assignee`, `sort:`, free text (title substring). PRs also take
  `review:none|required|approved|changes_requested`.
  `author:@me` and `assignee:@me` resolve to the signed-in user and match nothing for anonymous visitors.
- **Sorts**: `newest` (default), `oldest`, `updated`; issues also `comments`.
- **Out of scope**: saved filters (the URL is the saved filter), configurable page
  size, cursor pagination.
- **Pagination**: offset, fixed page size 25, `page` param, filtering and paging in SQL. `PaginationWithCount` shows the
  filtered total. Tab counts stay unfiltered. A `page` past the end clamps to the last page.
- **Pinned issues** show only on page 1 of the default view (`q` holds nothing beyond `is:`/`sort:`); hidden otherwise.
- **No JavaScript needed.** Every filter action is a full-page GET: chips, tabs and Clear filters are links, the search
  field and menus are GET forms. Menus are `<details>` panels with a search box and checkbox (labels, multi) or radio
  (single) inputs and an Apply button. With JavaScript, single-select menus submit on change and multi-select menus on
  close. No htmx fragment endpoint. Back, forward and shared URLs are plain navigation.
- **Rows**: issue rows get priority and comment count (`CommentStore.CountByIssueIDs`, batched for the page). PR rows
  already carry labels, comment count and CI. Row components are otherwise unchanged. Assignees are loaded for the
  filter only, not drawn.
- **Indexes**: one migration adds `issues(repo_id, state, created_at DESC)`, `pull_requests(repo_id, state, number
  DESC)`, `issue_labels(label_id)`, `pull_labels(label_id)`, `issue_assignees(user_id)`, `pull_assignees(user_id)` and
  `issues(milestone_id)`. Number: next free after 117 (09 may take 118); re-check after merging main.
- **Glossary**: add "Qualifier" to `GLOSSARY.md`; "filter chip" is a UI term only.
- **Copy**: the issues search placeholder (stripped of qualifier claims by ticket 08) becomes
  `Search or filter, e.g. is:open label:bug author:@me`, and the same on PRs.
- **Empty states** keep the filter-aware copy from the polish PR ("No open issues match these filters." plus Clear
  filters); PRs get the same, and a repo with none shows the empty state with a New button and no toolbar.

## Design

- Shared parser package `internal/listquery`: `Parse(raw string) Query`, `Query.String()` (canonical order, so equal
  filters give equal URLs), `Query.Without(qualifier)` and `Query.With(...)` for chip and menu links. Pure, no
  dependencies on stores.
- Stores take a typed filter (`IssueFilter` / `PullFilter`) built by the service from `Query`, resolving `@me` and
  milestone titles to IDs; return `(rows, total)` for the filtered set. Counts for tabs come from a separate unfiltered
  count query (replaces the Go loops).
- Draft vs open for PRs is a SQL predicate on the draft column, not a Go split.
- `ListWithCIStatus` takes real `limit, offset`; CI status (a ref resolve per PR) then runs for at most 25 rows.
- Shared view components (filter bar, chips, menu, empty state) in `internal/view/components/`, used by both pages.

## Tickets

Maintainer decision 2026-11-07: all four ship as one PR on `feat/issue-pr-list-filters` (`feat(issues,pulls): qualifier
search, filter menus and pagination`), one commit per ticket.

1. `01-listquery-parser` — parser package, tests, glossary entry. No UI.
2. `02-issue-list-filters` — issues SQL filter and paging, comment and priority rows, shared filter components, index
   migration, placeholder copy, standing mocks, benchmark. Blocked by 01.
3. `03-pr-list-filters` — PRs on the same pieces. Blocked by 01, 02.
4. `04-pr-review-filter` — `review:` filter, Review menu and review-state badge on PRs.

## Verification

Per ticket. Filters tested with and without JavaScript, back/forward and shared URLs, at 1440 in light and dark on a
throwaway server (`ada-silva/hollow-atlas`). SQL paging benchmarked with the large-repo harness before and after.
