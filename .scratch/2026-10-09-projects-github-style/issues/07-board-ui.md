# Board redesign: rich cards, composer, side panel

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent
Blocked by: 02, 03, 04, 05, 06

Spec: [../spec.md](../spec.md)

## Acceptance criteria

- [ ] Card face shows title, description preview, label chips, due date (danger when past), assignee avatars and linked issue/PR state; the whole card opens the editor, or navigates for a plain linked card.
- [ ] Side panel edits title, markdown description, assignees, labels, linked item and due date, with Save, Delete and Convert to issue; read-only users see no controls.
- [ ] Composer: Enter adds a titled card, `#` opens the picker, and the picker dropdown is not clipped by the board's scroll container.
- [ ] Column `⋯` menu with delete; dashed "Add column" slot; drag-to-move unchanged and never opens the panel.
- [ ] `make generate-templ` run; verified in the browser on a throwaway server with a scratch DB.
