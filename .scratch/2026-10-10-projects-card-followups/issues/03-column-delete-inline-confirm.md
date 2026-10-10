# Confirm Delete column inline, not with confirm()

Created: 2026-10-10
Category: enhancement
Status: done

## Problem

The column header `⋯` menu's "Delete column" asked with the browser's native `confirm()` (the `deleteColumnJS` templ script), while the card modal confirms Delete and Convert inline.

## Scope

- Choosing Delete column swaps the menu for a short confirmation in the same popover: "Delete this column and its N cards?" with the real card count, a destructive "Delete column" button (focused) and Cancel.
- Cancel and Escape return to the menu; Escape on the menu closes the popover. A second submit while the request runs is ignored, and a click outside or Escape is ignored until it settles.
- A failed request shows its error inline and keeps the popover open; success reloads the page.
- The logic is the `columnMenu` Alpine component in `kanban.js`, using `send()` (so it also times out after 30 s). `deleteColumnJS` is gone. Writers only, as before.

## Acceptance criteria

- [x] No `confirm(` is rendered on the board
- [x] The confirmation shows the column's card count; absent for read-only viewers
- [x] Cancel, Escape, the double-submit guard and the inline error behave as described, checked in the browser
- [x] Confirming deletes the column and its cards
