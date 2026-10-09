# Board redesign: columns, composer, clickable cards

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent
Blocked by: 02, 03

Spec: [../spec.md](../spec.md)

## Acceptance criteria

- [ ] Column header shows count and a `⋯` menu with delete; "add column" is a dashed slot after the last column.
- [ ] Inline composer per column: Enter adds a note; `#` opens the issue/PR picker and links the selection. No `prompt()` left.
- [ ] Issue/PR cards navigate on click; note cards open an edit/delete dialog. A drag never triggers navigation.
- [ ] Drag-to-move, read-only view (no write controls) and closed-project view still work.
- [ ] `make generate-templ` run; verified in the browser on a throwaway server.
