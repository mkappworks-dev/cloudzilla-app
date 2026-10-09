# Notifications

In-app notifications for activity on issues, PRs and discussions you opened, @-mentions of you, and repositories you watch, with optional email delivery (see [Email](#email)).

## Notification Types

| Type               | Triggered when                              |
| ------------------ | ------------------------------------------- |
| `issue_comment`    | Someone comments on an issue you opened     |
| `pr_comment`       | Someone comments on a PR you opened         |
| `issue_closed`     | Someone closes an issue you opened, by hand or with a closing keyword in a merged PR or pushed commit |
| `issue_reopened`   | Someone reopens an issue you opened         |
| `pr_merged`        | Someone merges a PR you opened              |
| `pr_closed`        | Someone closes a PR you opened              |
| `pr_review`        | Someone reviews a PR you opened             |
| `mention`          | Someone @-mentions you in a comment         |
| `discussion_reply` | Someone replies to a discussion you started |
| `repo_transfer`    | Someone wants to transfer a repository to you; links to `/repos/transfers` |
| `pr_opened`        | (type reserved; not currently auto-fired)   |

Notifications are never created when `actorID == authorID` (self-actions are silent). Every notification, watchers' copies included, is created only if its recipient can read the repo at that moment (`RepoService.CanRead`), so watching a repo or having opened the issue, PR or discussion stops counting once the user is removed as a collaborator, removed from the org or demoted from org owner. `CommentService` likewise records an @-mention only for users who can read the repo. The exception is `repo_transfer`: its recipient can't read a private repo until they accept it.

## Who is notified

Comments, reviews, state changes and discussion replies go through `NotificationService.notifyThread`. The recipients are the thread's author, its `subscribed` rows (see `ThreadSubscriptionService`) and, except for discussion replies, the repo's non-ignoring watchers. It drops the actor, users with a `muted` row on the thread, and anyone who can't read the repo. A subscriber needn't watch the repo, and an `ignoring` watcher who subscribed to the thread is still notified. An author with no row, such as one on a thread that predates subscriptions, is still notified; a muted author is not.

Discussion replies never reached watchers, so they still don't: author and subscribers only.

`NotifyMention` ignores a mute. After creating the notification it calls `SubscribeOnMention`, which flips the row to `subscribed`. A mention for a user who can't read the repo creates neither.

A failure to list muted users or subscribers is logged and the fan-out is skipped, rather than notifying someone who muted the thread.

## Auto-subscribe

Taking part in a thread subscribes you to it through `ThreadSubscriptionService.AutoSubscribe`, which inserts only if you have no row: the first reason is kept and a mute is never undone.

| Action | Service | Reason | Who |
| ------ | ------- | ------ | --- |
| Open an issue, PR or discussion | `IssueService.Create`, `PullService.Create`, `DiscussionService.Create` | `author` | the author |
| Comment on an issue or PR, reply to a discussion | `CommentService.CreateForIssue` / `CreateForPull`, `DiscussionService.CreateReply` | `comment` | the commenter |
| Submit a PR review | `PullReviewService.SubmitReview` | `review` | the reviewer |
| Assign someone | `AssigneeService.AddToIssue` / `AddToPull` | `assign` | the assignee, not the actor |

Each service gets the subscriber through `WithThreadSubscriptions`; without it nothing is recorded. A failed write is logged and the request still succeeds. Mentions subscribe through `NotifyMention` instead. Threads that existed before the feature get no rows.

## Thread kind

`notifications.subject_kind` is `issue`, `pull` or `discussion`: the kind of thread `subject_id` numbers, since the number alone doesn't say. Every `Notify*` call sets it except `NotifyRepoTransfer`, which leaves it NULL. Migration 114 backfilled older rows from `type`, and mentions from the section of `subject_url`; a mention whose URL names no section stays NULL. `model.Notification.SubjectKind` is empty for NULL.

## Email

Sent only when SMTP is configured. Users set these on `/settings#notifications` (saved by POST `/settings/notifications`); they never affect in-app notifications.

| `users` column        | Effect                                                                                                                          |
| --------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `email_notifications` | Master switch; off means no email at all                                                                                        |
| `email_digest`        | `immediate` (one email per notification addressed to you), `daily` / `weekly` (digest at 08:00 UTC, weekly on Mondays), `never` |
| `notify_mention`      | Off suppresses email for `mention`                                                                                              |
| `notify_pr_review`    | Off suppresses email for `pr_review`                                                                                            |

`wantsEmail(user, type, digestMode)` in `internal/service/email_service.go` decides per notification, and says no for a [suspended account](./access-control.md#suspended-accounts): `EmailService.SendNotification` calls it with `immediate`, and `NotificationService.ListUnreadForDigest` calls it with the digest mode. The digest job (`runEmailDigest` in `cmd/server/main.go`) first narrows users with `UserService.ListUsersForDigest`, whose SQL repeats the master-switch and digest-mode check.

Immediate email goes to the notification's direct recipient (the subject's author, or the mentioned user) and to users with a `subscribed` row on the thread. Watchers without a row get in-app copies of issue/PR notifications; those are never emailed immediately, but digests draw from all unread notifications, so daily/weekly users also get watched-repo activity, and `notify_pr_review` filters watched-repo reviews there too. The digest re-checks read access (`NotificationStore.ListUnreadReadable`, with `readableBy`) and leaves out notifications on repos the user can no longer read, keeping a `repo_transfer` notification while its transfer is still offered to the user.

## Unread Count in Navbar

`basePage()` calls `NotificationService.CountUnread` on every page render and passes the count as `BasePage.UnreadNotifCount`. Templates show a badge next to the Notifications link.

## Pages & API

| Route / Endpoint                      | Auth     | Description                                   |
| ------------------------------------- | -------- | --------------------------------------------- |
| GET `/notifications`                  | Required | Inbox, 25 per page: `?filter=inbox\|unread\|read` (unknown means inbox), `?page=N` (clamped to the last page) |
| PATCH `/api/notifications/{id}`       | Required | Mark single notification as read (HTMX-aware) |
| POST `/api/notifications/read-all`    | Required | Mark all notifications as read (HTMX-aware)   |
| GET `/api/notifications/unread-count` | Required | Returns `{"count": N}` JSON                   |
| POST `/api/notifications/done`        | Required | Mark the notifications in repeated form field `ids` as read (at most 100; other users' ids are ignored) |
| POST `/api/notifications/unsubscribe` | Required | Stop watching the repo behind each notification in `ids` |

The mark-read and bulk endpoints take the same `?filter` and `?page`, and their HTMX responses swap `pages.NotificationsInbox` into `#notifications-view`, so the view stays on the caller's page.

Each row has a checkbox, the actor's avatar and, on hover or focus, Done and Unsubscribe buttons; ticking rows swaps the list header for a bulk bar. Unsubscribe is per repo, not per thread: no thread-level subscriptions exist, so it deletes the user's watch on the repo. Every row has the button, but it is active only where that watch exists at a level other than `ignoring`; elsewhere, and on `repo_transfer` rows (whose repo says nothing about the user's watches), it is disabled with a tooltip saying why. Done is mark-read: the row stays in the inbox and moves to the Read filter.

Rows are grouped under their repo within a page. A row shows `subject_title` when set, with the "@actor did X" text beside it; mentions, repo transfers and rows created before migration 112 have no title and show only the action text.

## NotificationService (`internal/service/notification_service.go`)

- `ListPage(ctx, userID, filter, page)` → `(NotificationPage, error)` — one page plus the inbox/unread/read counts the sidebar shows
- `List(ctx, userID)` → `([]Notification, error)` — newest 50
- `ListUnreadForDigest(ctx, u, mode)` → `([]Notification, error)`
- `CountUnread(ctx, userID)` → `(int, error)`
- `MarkRead(ctx, id, userID)` → `error`
- `MarkAllRead(ctx, userID)` → `error`
- `NotifyIssueComment(ctx, repo, issue, actorID, actorName)` — call from `CreateIssueComment` handler
- `NotifyPRComment(ctx, repo, pr, actorID, actorName)` — call from `CreatePullComment` handler
- `NotifyIssueStateChange(ctx, repo, issue, actorID, actorName)` — call from `UpdateIssue` handler; `IssueCloser` calls it for each issue a closing keyword closes, with the merger or pusher as actor
- `NotifyPRStateChange(ctx, repo, pr, actorID, actorName)` — call from `UpdatePull` handler
- `NotifyPRReview(ctx, repo, pr, actorID, actorName)` — call from `SubmitReview` handler
- `NotifyDiscussionReply(ctx, repo, discussion, actorID, actorName)` — call from `CreateReply` handler; author and subscribers only
- `WithThreadSubscriptions(svc)` — builder; without it no thread rows are consulted
- `NotifyMention(ctx, repo, actorID, actorName, mentionedUserID, kind, subjectNumber, subjectURL)` — called by `CommentService` for each readable @-mention in a new issue/PR comment; `kind` is `model.ThreadKindIssue` or `ThreadKindPull`
- `NotifyRepoTransfer(ctx, transfer)` — call from `TransferRepo` handler when the transfer awaits its recipient; `SubjectID` is the transfer ID. It skips the `CanRead` check, since the recipient can't read a private repo until they accept

Handlers run the `Notify*` calls in a goroutine with `context.WithoutCancel(r.Context())`: net/http cancels the request context as soon as the handler returns, which would abort the inserts and the email.
