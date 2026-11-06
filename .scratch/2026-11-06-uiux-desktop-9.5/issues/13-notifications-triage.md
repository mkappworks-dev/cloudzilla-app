# Notifications triage

Created: 2026-11-06
Category: enhancement
Status: done
Blocked by: 02

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `feat/notifications-triage` | `.worktrees/feat+notifications-triage` | `feat(notifications): group by thread, filter by reason, keyboard shortcuts` | 2 | M | IA +0.3, Features +0.2 |

## Problem

The inbox lists every notification flat with no way to narrow it; Unsubscribe mutes but rows stay.

## Scope

- Group by thread; filter chips for reason (mention, review requested, assigned, subscribed).
- `j`/`k` to move, `e` to mark done, `?` lists shortcuts.
- Mock first in `mockups/notifications.html` or a `.scratch` mock.

## Files

- `internal/view/pages/notifications.templ`
- `internal/handler/notification_page_handler.go`
- `internal/service/notification_service.go`
- `internal/store/notification_store.go`

## Acceptance criteria

- [x] Grouping and reason filters work with pagination
- [x] Shortcuts work and are listed

## Verification

Seeded server; siteadmin has 1,400+ notifications.

## Notes

Read docs/notifications.md first.

## Comments

2026-11-06 (triage): ready-for-agent.

2026-11-06 (wave 1 re-score, ticket 22): Findings in this ticket's files, left for it: row checkboxes all read
"Select notification" instead of naming the item (`notifications.templ:279`); "Done" and unsubscribe are revealed only
on hover or focus (`:294`, Codex +0.2); no filter by repository, reason or text on 1,346 notifications (Codex +0.5);
per-repo groups repeat the same discussion title five times with no collapse; no clear-older action (Claude).
