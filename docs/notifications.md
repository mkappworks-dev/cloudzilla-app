# Notifications

In-app notification system that creates notifications for issue/PR activity involving the author.

## Notification Types

| Type             | Triggered when                            |
| ---------------- | ----------------------------------------- |
| `issue_comment`  | Someone comments on an issue you opened   |
| `pr_comment`     | Someone comments on a PR you opened       |
| `issue_closed`   | Someone closes an issue you opened        |
| `issue_reopened` | Someone reopens an issue you opened       |
| `pr_merged`      | Someone merges a PR you opened            |
| `pr_closed`      | Someone closes a PR you opened            |
| `pr_opened`      | (type reserved; not currently auto-fired) |

Notifications are never created when `actorID == authorID` (self-actions are silent).

## Unread Count in Navbar

`basePage()` calls `NotificationService.CountUnread` on every page render and passes the count as `BasePage.UnreadNotifCount`. Templates show a badge next to the Notifications link.

## Pages & API

| Route / Endpoint                      | Auth     | Description                                   |
| ------------------------------------- | -------- | --------------------------------------------- |
| GET `/notifications`                  | Required | Notifications page (list all)                 |
| PATCH `/api/notifications/{id}`       | Required | Mark single notification as read (HTMX-aware) |
| POST `/api/notifications/read-all`    | Required | Mark all notifications as read (HTMX-aware)   |
| GET `/api/notifications/unread-count` | Required | Returns `{"count": N}` JSON                   |

HTMX responses swap `fragment-notifications-list` into `#notifications-list`.

## NotificationService (`internal/service/notification_service.go`)

- `List(ctx, userID)` → `([]Notification, error)`
- `CountUnread(ctx, userID)` → `(int, error)`
- `MarkRead(ctx, id, userID)` → `error`
- `MarkAllRead(ctx, userID)` → `error`
- `NotifyIssueComment(ctx, repo, issue, actorID, actorName)` — call from `CreateIssueComment` handler
- `NotifyPRComment(ctx, repo, pr, actorID, actorName)` — call from `CreatePRComment` handler
- `NotifyIssueStateChange(ctx, repo, issue, actorID, actorName)` — call from `UpdateIssue` handler
- `NotifyPRStateChange(ctx, repo, pr, actorID, actorName)` — call from `UpdatePull` handler
