# Cap the topbar org and repo switcher dropdowns

Created: 2026-10-09
Category: enhancement
Status: done

## Problem

The topbar account/org and repo dropdowns list every org membership and every repo the owner has, so they grow without bound.

## Acceptance criteria

- [x] Each dropdown lists at most 8 entries (`switcherLimit` in `internal/view/layout/layout.templ`)
- [x] When entries are hidden, the footer link reads "View all N organizations" (`/organizations`) or "View all N repositories" (`/{owner}?tab=repositories`)
- [x] The repo footer link goes to the current owner's repositories, not the viewer's own `/repos`
- [x] The topbar avatar lookup still sees the full org list
- [x] Render test in `internal/view/layout/layout_test.go`

## Comments
