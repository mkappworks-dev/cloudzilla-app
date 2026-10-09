# Notifications page, GitHub-style

Created: 2026-10-09
Category: enhancement
Status: done

Redesign `/notifications` as a GitHub-style inbox, with real subject titles and numbered pagination. Approved mock: a two-pane layout (filter sidebar + list), rows grouped by repo, hover-revealed "Done" action, 25 per page.

## Scope

- Sidebar filters: Inbox (all), Unread, Read; `?filter=` query param.
- Rows grouped under repo headers; unread dot, type icon, title, time. The mock's row checkboxes are dropped: with no bulk action they'd be dead controls.
- Numbered pagination, 25 per page, `?page=N`, "1–25 of N" caption.
- Subject titles: `notifications.subject_title` stored when the notification is created. Older rows have none and keep the current "@actor did X #N" text.
- Mark one / mark all read keep the current page and filter.

## Out of scope

- Type and per-repo sidebar filters (mock shows them; follow-up).
- Bulk select actions beyond mark-all-read.
- Backfilling titles for existing rows.

## Tickets

1. `issues/01-subject-title.md`
2. `issues/02-paginated-inbox-ui.md`
