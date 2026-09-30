# Notifications

In-app notifications for activity on issues, PRs and discussions you opened, @-mentions of you, and repositories you watch, with optional email delivery (see [Email](#email)).

## Notification Types

| Type               | Triggered when                              |
| ------------------ | ------------------------------------------- |
| `issue_comment`    | Someone comments on an issue you opened     |
| `pr_comment`       | Someone comments on a PR you opened         |
| `issue_closed`     | Someone closes an issue you opened          |
| `issue_reopened`   | Someone reopens an issue you opened         |
| `pr_merged`        | Someone merges a PR you opened              |
| `pr_closed`        | Someone closes a PR you opened              |
| `pr_review`        | Someone reviews a PR you opened             |
| `mention`          | Someone @-mentions you in a comment         |
| `discussion_reply` | Someone replies to a discussion you started |
| `pr_opened`        | (type reserved; not currently auto-fired)   |

Notifications are never created when `actorID == authorID` (self-actions are silent). A `mention` is recorded and notified only when the mentioned user can read the repo (`RepoService.CanRead`).

## Email

Sent only when SMTP is configured. Users set these on `/settings#notifications` (saved by POST `/settings/notifications`); they never affect in-app notifications.

| `users` column        | Effect                                                                                                                          |
| --------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `email_notifications` | Master switch; off means no email at all                                                                                        |
| `email_digest`        | `immediate` (one email per notification addressed to you), `daily` / `weekly` (digest at 08:00 UTC, weekly on Mondays), `never` |
| `notify_mention`      | Off suppresses email for `mention`                                                                                              |
| `notify_pr_review`    | Off suppresses email for `pr_review`                                                                                            |

`wantsEmail(user, type, digestMode)` in `internal/service/email_service.go` decides per notification: `EmailService.SendNotification` calls it with `immediate`, and `NotificationService.ListUnreadForDigest` calls it with the digest mode. The digest job (`runEmailDigest` in `cmd/server/main.go`) first narrows users with `UserService.ListUsersForDigest`, whose SQL repeats the master-switch and digest-mode check.

Immediate email goes only to the notification's direct recipient (the subject's author, or the mentioned user). Watchers also receive in-app copies of issue/PR notifications (`fanOutToWatchers`); those are never emailed immediately, but digests draw from all unread notifications, so daily/weekly users also get watched-repo activity, and `notify_pr_review` filters watched-repo reviews there too.

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
- `ListUnreadForDigest(ctx, u, mode)` → `([]Notification, error)`
- `CountUnread(ctx, userID)` → `(int, error)`
- `MarkRead(ctx, id, userID)` → `error`
- `MarkAllRead(ctx, userID)` → `error`
- `NotifyIssueComment(ctx, repo, issue, actorID, actorName)` — call from `CreateIssueComment` handler
- `NotifyPRComment(ctx, repo, pr, actorID, actorName)` — call from `CreatePullComment` handler
- `NotifyIssueStateChange(ctx, repo, issue, actorID, actorName)` — call from `UpdateIssue` handler
- `NotifyPRStateChange(ctx, repo, pr, actorID, actorName)` — call from `UpdatePull` handler
- `NotifyPRReview(ctx, repo, pr, actorID, actorName)` — call from `SubmitReview` handler
- `NotifyDiscussionReply(ctx, repo, discussion, actorID, actorName)` — call from `CreateReply` handler
- `NotifyMention(ctx, repo, actorID, actorName, mentionedUserID, subjectNumber, subjectURL)` — called by `CommentService` for each readable @-mention in a new issue/PR comment

Handlers run the `Notify*` calls in a goroutine with `context.WithoutCancel(r.Context())`: net/http cancels the request context as soon as the handler returns, which would abort the inserts and the email.
