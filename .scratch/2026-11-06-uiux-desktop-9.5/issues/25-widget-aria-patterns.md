# Keyboard and ARIA patterns for custom widgets

Created: 2026-11-06
Category: bug
Status: needs-triage
Blocked by: —

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `fix/widget-aria-patterns` | `.worktrees/fix+widget-aria-patterns` | `fix(a11y): keyboard and ARIA patterns for custom widgets` | polish (wave 1 re-score) | M | A11y +0.5 |

## Problem

Found by the wave 1 re-score (Codex).

1. The Markdown editor's Write/Preview tabs lack the tab pattern: no tab/panel association, no roving tabindex, no
   arrow keys (`internal/view/components/markdown_editor.templ:10`, `:43`; `internal/view/layout/layout.templ:443`).
2. Home's filter and sort menus declare menu roles but have no arrow-key navigation, Escape, or focus transfer on open
   (`internal/view/pages/home.templ:95`, `:132`). Check `cmd/server/frontend/static/select-menu.js` users for the same.
3. (Claude, unverified) Several raw inputs use `focus:` instead of `focus-visible:` for their ring, unlike buttons.

Ticket 12 gates what axe can detect; none of these are automated findings, so they stay open after it.

## Scope

- Implement the WAI-ARIA tabs pattern in the editor, and menu keyboard behaviour (arrows, Home/End, Escape, focus return)
  in the shared menu code rather than per page.
- Align focus-ring rules between inputs and buttons.

## Files

- `internal/view/components/markdown_editor.templ`, `internal/view/layout/layout.templ` (editor script only; 14 owns the
  header)
- `internal/view/pages/home.templ`, `cmd/server/frontend/static/select-menu.js`

## Acceptance criteria

- [ ] Editor tabs: arrow keys move between tabs, `aria-controls`/`aria-labelledby` pair tab and panel
- [ ] Every menu opens, navigates and closes by keyboard and returns focus to its trigger
- [ ] Keyboard pass on the editor and home menus, recorded in the PR

## Verification

Throwaway server at 1440×900 in light and dark, per the harness in [spec.md](../spec.md).

## Comments

2026-11-06 (wave 1 re-score): opened by ticket 22. Run `/triage` before any code.
