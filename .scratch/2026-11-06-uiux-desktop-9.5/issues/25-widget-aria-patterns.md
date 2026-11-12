# Keyboard and ARIA patterns for custom widgets

Created: 2026-11-06
Category: bug
Status: done
Blocked by: 24

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

- [x] Editor tabs: arrow keys move between tabs, `aria-controls`/`aria-labelledby` pair tab and panel
- [x] Every menu opens, navigates and closes by keyboard and returns focus to its trigger
- [x] Keyboard pass on the editor and home menus, recorded in the PR

## Verification

Throwaway server at 1440×900 in light and dark, per the harness in [spec.md](../spec.md).

## Comments

2026-11-06 (wave 1 re-score): opened by ticket 22. Run `/triage` before any code.

2026-11-12 (triage): ready-for-agent. Run after the wave 2 re-score (22).

2026-11-12 (wave 2 re-score): Wave 2 re-score, same area: icon-only controls (the ~8px × on review comments, copy buttons, tree/list toggles, edit and add icons) have no visible name and small targets, accessible names unverified (Claude and Codex); inputs that rely on a placeholder as their label (collaborator Username, gist filename, "type your username", admin invitation fields); unnamed initials avatars in Participants and Reviewers (`pull-*`, `repo-settings`, `gist-new`); the review dialog textarea has a screen-reader label but no hint that a summary is required for Request changes. Check each for an accessible name, a 24px target and a visible label.

2026-11-12: waits for 24, which also edits `home.templ`.

2026-11-12 (done): editor tabs follow the WAI-ARIA tabs pattern with automatic activation; one delegated handler in
`select-menu.js` gives every `role=menu` popup arrows, Home/End, Escape, Tab and focus return. Item 3 needed no change:
every `focus:ring` is on a text control, which always matches `:focus-visible`, and no button or link uses `focus:`.
Wave 2: every icon-only control already had an accessible name; targets raised to 24px, placeholder-only inputs got
visible labels (collaborator, invite, verify email, delete account), reviewer initials are named, and the review dialog
says a summary is needed.
