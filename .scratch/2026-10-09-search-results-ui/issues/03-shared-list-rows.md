# Shared list row components

Created: 2026-10-09
Category: enhancement
Status: done
Blocked by: none

## What

Extract the row markup that lives inline in two pages into components, with no visual change to those pages:

- `components.IssueListRow` from `pages/issues.templ` (state icon, private lock, title, `#N · opened by · time`, labels, state badge).
- `components.RepoListRow` from `pages/explore.templ` (`owner/name`, description, optional Private badge, optional stats slot, optional created date).
- `components.PersonListRow` (avatar, linked name, one muted line) for users and orgs.

Row data structs follow `PRListRowData`: pre-formatted strings, so the components don't depend on models. `PRListRow` gains an optional `ShowRepo` prefix for lists that span repos.

## Acceptance criteria

- [x] The repo issues page and Explore look unchanged on a seeded DB (checked visually; Explore's list is now a `ul`/`li` instead of `div`s).
- [x] Each component has a render test like `pr_list_row_test.go`.
- [x] No search code changes in this ticket.

## Relevant files

`internal/view/pages/issues.templ`, `internal/view/pages/explore.templ`, `internal/view/components/pr_list_row.templ`
