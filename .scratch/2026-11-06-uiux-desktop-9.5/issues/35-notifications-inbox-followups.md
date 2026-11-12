# Notifications inbox follow-ups

Created: 2026-11-12
Category: enhancement
Status: done
Blocked by: —

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `fix/notifications-inbox-followups` | `.worktrees/fix+notifications-inbox-followups` | `fix(ui): notifications inbox follow-ups` | polish (wave 2 re-score) | S | Interaction +0.2 |

## Problem

Found by the wave 2 re-score (Claude and Codex).

1. Select-all selects only the 25 rows on the page, with no "select all 372" (Claude, `notifications-selected`).
2. The header says "Mark all as read" while the selection action and the shortcuts dialog say "Done" and "Mark as done"; nothing says whether Done means read, archived or resolved, and there is no mark-unread action (Codex and Claude).
3. With every notification unread, every row is tinted, so unread no longer distinguishes rows; selected rows are only a shade darker; the per-row count pill is unlabeled.

## Scope

- "Select all N" across pages, one verb for read/done, a mark-unread action and shortcut, a clearer selected and unread treatment.
- Follow-up to ticket 13 (merged).

## Files

- `internal/view/pages/notifications.templ`
- notification handlers and store
- `mockups/notifications.html`

## Acceptance criteria

- [x] Select all across pages
- [x] One verb for done/read, documented in the shortcuts dialog
- [x] Mark unread
- [x] Mocks updated, light and dark

## Verification

Throwaway server at 1440×900 in light and dark, per the harness in [spec.md](../spec.md).

## Comments

2026-11-12 (wave 2 re-score): opened by ticket 22. Run `/triage` before any code.

2026-11-12 (triage): ready-for-agent. Pick one verb for Done/read and use it in the header, selection bar and shortcuts dialog.
