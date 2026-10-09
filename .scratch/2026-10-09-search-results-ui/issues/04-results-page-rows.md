# Results page uses the shared rows

Created: 2026-10-09
Category: enhancement
Status: done
Blocked by: 01-org-results, 02-result-context, 03-shared-list-rows

## What

Rebuild the groups in `pages/search.templ` from the row components: Repositories (`RepoListRow`, Private badge, no Public badge), Organizations and Users (`PersonListRow`), Issues (`IssueListRow` with an `owner/repo` prefix), Pull Requests (`PRListRow`, empty CI, labels and reviewers). Group order: Repositories, Organizations, Issues, Pull Requests, Users. Each group is one bordered section with a header, as on the main pages. The empty state also checks `Orgs`. Move the repeated per-type visibility conditions into a small helper instead of repeating them four times.

## Acceptance criteria

- [x] `/search?q=brave` shows all five groups on a seeded DB, each row links to its item.
- [x] `type=orgs` shows only organizations; the tab count matches.
- [x] Private repos show the Private badge; there is no Public badge.
- [x] No-results state still renders when all five lists are empty.
- [x] Page-render test via the router harness asserts a link per type.

## Relevant files

`internal/view/pages/search.templ`, `internal/view/viewmodels_social.go`
