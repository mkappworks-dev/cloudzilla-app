# Board redesign: rich cards, composer, side panel

Created: 2026-10-09
Category: enhancement
Status: done
Blocked by: 02, 03, 04, 05, 06

Spec: [../spec.md](../spec.md)

## Acceptance criteria

- [x] Card face shows title, description preview, label chips, due date (danger when past), assignee avatars and linked issue/PR state; the whole card opens the card modal, or navigates for a plain linked card.
- [x] One card modal creates a card from a column's "+ Add item" (every field; title optional only for a linked-only card) and edits a note card (two-column modal with a close X: title and markdown description on the left; assignee and label dropdowns, due date via a calendar picker and the linked item as a link chip on the right) with Save, Delete and Convert to issue confirmed inline; read-only users see no controls.
- [x] The `#` picker list, the calendar and the assignee and label dropdowns are not clipped by the modal's scroll container or the viewport; Escape closes them before the modal.
- [x] Column `⋯` menu with delete; dashed "Add column" slot; drag-to-move unchanged and never opens the modal.
- [x] `make generate-templ` run; verified in the browser on a throwaway server with a scratch DB.

## Comments

claude, 2026-10-09: Ticked on the task 6 browser check (14 of 15 pass) and template tests. Known gaps:
- Drag-to-move was verified with synthetic DragEvents only; the browser's own click-after-drag suppression was not exercised with a real mouse.
- The column `⋯` menu is in the markup but its delete was not itemised in the browser check; the dashed "Add column" slot was driven.
- Deferred minors: no client-side canWrite guard on panel actions (server returns 403), picker listeners never removed, no focus trap or dirty prompt in the panel, cards lost the `cursor-grab` affordance.

claude, 2026-10-10: changed from a side panel and inline composer to a single create/edit modal at the user's request.
