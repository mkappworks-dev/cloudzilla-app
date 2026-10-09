# Projects: GitHub-style list and board

Created: 2026-10-09
Category: enhancement
Status: needs-triage

## Problem

- `/{owner}/{repo}/projects`: only the title of a row is a link, and "New project" toggles an inline form.
- `/{owner}/{repo}/projects/{id}`: cards are not clickable as a whole, notes cannot be edited, "+ Add card" uses `prompt()` and an `i5`/`p3` syntax, and "+ Column" is a header button.

## Design

1. **List** (`internal/view/pages/projects.templ`): the whole row is a link (stretched-link overlay on the title; Delete stays independently clickable). "New project" opens a dialog (`components` dialog) with name and description, POSTs, then navigates to the new board.
2. **Board** (`internal/view/pages/project_detail.templ`, `cmd/server/frontend/kanban.js`):
   - Columns get a count and a `⋯` menu (delete column); "add column" becomes a dashed slot after the last column.
   - Per-column inline composer replaces `prompt()`: Enter adds a note; typing `#` searches this repo's issues/PRs and links the pick.
   - Issue/PR cards navigate to the issue/PR on click; note cards open a dialog to edit or delete. Drag-to-move is unchanged. Clicks that end a drag must not navigate.
3. **Backend**: edit a note card (`PATCH .../cards/{cardID}` extension or a sibling route; store + service `UpdateCardNote`, write-access guard like `DeleteCard`), and a JSON search endpoint for repo issues/PRs by title or number (reuse existing search if present).

## Open choices (defaults taken)

- Note-card click: dialog, not side panel.
- Linking: `#` picker in the composer, not a separate dialog.

## Testing

Service tests for note edit and picker query; router test for permissions (read-only user gets 403/404); browser check on a throwaway server and scratch DB, not the shared dev DB.

## Tickets

`issues/01` list page, `issues/02` note edit backend, `issues/03` issue/PR picker endpoint, `issues/04` board UI.
