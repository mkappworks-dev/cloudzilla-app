# Row selection, Done and Unsubscribe, actor avatars

Created: 2026-10-09
Category: enhancement
Status: done
Blocked by: 02

## What

Per `mock-row-actions.html`: each row gets a checkbox, the actor's avatar and, on hover or focus, ✓ Done and 🔕 Unsubscribe buttons in place of the date. Ticking a row swaps the list header for "N selected · Done · Unsubscribe".

- Done is mark-read; read rows stay in the inbox and move to the Read filter.
- Unsubscribe is repo-level: it deletes the user's watch on the notification's repo (no per-thread subscriptions exist). On every row, but disabled (with a tooltip) unless the user has a non-`ignoring` watch, and always on `repo_transfer` rows.
- `POST /api/notifications/done` and `POST /api/notifications/unsubscribe` take repeated `ids`; both only touch the caller's own notifications.

## Acceptance criteria

- [x] Hover or keyboard focus on a row shows Done (unread rows) and Unsubscribe (watched repos); the date returns on leave.
- [x] Selecting rows shows the bulk bar; select-all covers the current page only.
- [x] Bulk Done marks exactly the selected, owned notifications read; other users' ids are ignored.
- [x] Unsubscribe deletes the watch for each distinct repo among the selected notifications and leaves unrelated repos alone.
- [x] Avatars use `components.Avatar` and fall back to initials.
- [x] Both endpoints re-render the inbox on the caller's filter and page.
- [x] Store, service and render tests; `docs/notifications.md` documents the endpoints.
