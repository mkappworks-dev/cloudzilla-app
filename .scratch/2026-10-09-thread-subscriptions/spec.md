# Per-thread notification subscriptions

Created: 2026-10-09
Category: enhancement
Status: needs-triage

GitHub-style subscriptions for single issues, pull requests and discussions. A user can follow one thread in a repo they don't watch, and mute one thread in a repo they do. Approved mock: `mock.html` (sidebar control, inbox row, fan-out table).

## Behaviour

- **Thread row**: `thread_subscriptions (user_id, repo_id, kind, number, state, reason, created_at, updated_at)`, primary key `(user_id, repo_id, kind, number)`. `kind` is `issue|pull|discussion`, `state` is `subscribed|muted`, `reason` is `author|comment|review|mention|assign|manual`. Both user and repo FKs cascade.
- **Effective state** shown to the viewer: the thread row if one exists; otherwise "subscribed (watching)" when their repo watch is `watching`; otherwise not subscribed.
- **Auto-subscribe** on authoring, commenting, reviewing, being assigned and being @-mentioned. Author, comment, review and assign insert `ON CONFLICT DO NOTHING`, so they never undo a mute. A mention upserts to `subscribed`, and so does the manual Subscribe button.
- **Fan-out** for a new comment, review or state change on a thread: recipients are the thread's `subscribed` users plus the repo's non-ignoring watchers plus the author, minus the actor, minus `muted` users, minus anyone who can't read the repo (existing `CanRead` check). The author is still notified on threads that predate this feature.
- **Mention breaks through a mute**: `NotifyMention` ignores `muted` and re-subscribes the user.
- **Email**: immediate email goes to the direct recipient as today, and now also to thread subscribers who have a `subscribed` row. Watchers without a row stay in-app and digest only.
- **Digest** respects mutes because muting marks the thread's unread notifications read, and a muted thread creates no new ones.
- **Sidebar control** on issue, PR and discussion pages replaces the PR page's repo-watch Subscribe button. It reads and writes the thread row and never touches the repo watch.
- **Inbox**: the row and bulk Unsubscribe buttons mute the notification's thread (and mark its notifications read) instead of deleting the repo watch. `repo_transfer` rows stay disabled. Stopping a repo watch is then only possible from the repo page's Watch control.

## API

`PUT /api/repos/{owner}/{repo}/{issues|pulls|discussions}/{number}/subscription` with `{"state":"subscribed"|"muted"}`. Requires login and repo read access. HTMX requests get the re-rendered sidebar section. `POST /api/notifications/unsubscribe` keeps its path and `ids` parameter; its meaning changes to "mute the threads behind these notifications".

## Design notes

- `notifications` gains `subject_kind` (nullable), set by every `Notify*` call. Rows older than the migration are backfilled from `type` and, for mentions, from `subject_url`. The inbox needs it because `subject_id` is only the number.
- `ThreadSubscriptionService` (new) owns `Status`, `Set`, `AutoSubscribe`, `MutedUsers`, `SubscribedUsers` and `MuteFromNotifications`. `NotificationService` and the issue, PR, comment, assignee and discussion services depend on it. Auto-subscribe failures are logged and never fail the request.
- The three kinds share one table and one fragment; no per-kind duplication.

## Out of scope

- Org-level or user-level watching.
- A "Watching" settings page listing every thread subscription.
- Notifying on assignment (the assignee is subscribed but no notification type is added).
- Subscribing to releases, commits or other subjects.
- Cleaning up rows when an issue, PR or discussion is deleted (they are orphaned and harmless).

## Risks

- The PR page's Subscribe button is being reworded in a separate PR. If it lands first, ticket 04 rebases over it.
- Moving an inbox button from repo-level to thread-level changes what existing users' muscle memory does. The row tooltip says "mute this thread. Repo watch unchanged."

## Tickets

1. `issues/01-data-layer.md`
2. `issues/02-fan-out.md`
3. `issues/03-auto-subscribe.md`
4. `issues/04-sidebar-control.md`
5. `issues/05-inbox-mute.md`
