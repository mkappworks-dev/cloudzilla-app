# GitHub-style inbox with filters and pagination

Created: 2026-10-09
Category: enhancement
Status: done
Blocked by: 01

## What

`NotificationStore.ListPage(ctx, userID, filter, limit, offset)` and `CountByFilter`; service and `PageNotifications` take `?filter=inbox|unread|read` and `?page=N`. Rebuild `pages/notifications.templ` and the HTMX fragment to the approved mock, showing `SubjectTitle` when present. Mark-read endpoints re-render the same page and filter.

## Acceptance criteria

- [x] 25 notifications per page; a page past the end clamps to the last page.
- [x] Filters change the list and the total; the navbar unread badge is unchanged.
- [x] Rows group by repo within a page, newest first.
- [x] Marking read on page 2 stays on page 2.
- [x] Store, service and page-render tests cover filter, offset, clamp and pager links.
- [x] `docs/notifications.md` documents `?filter` and `?page`.
