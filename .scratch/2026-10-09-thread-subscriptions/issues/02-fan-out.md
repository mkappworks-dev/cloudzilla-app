# Fan-out respects thread subscriptions

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent
Blocked by: 01

## What

Rework recipient selection in `internal/service/notification_service.go` so `NotifyIssueComment`, `NotifyPRComment`, `NotifyIssueStateChange`, `NotifyPRStateChange`, `NotifyPRReview` and `NotifyDiscussionReply` share one path. It sends to subscribers, watchers and the author, and drops the actor and muted users. `NotifyDiscussionReply` gets watcher fan-out only if it already has it today (it currently notifies just the author); otherwise it gains subscriber fan-out only.

- `NotifyMention` ignores a mute and calls `SubscribeOnMention`.
- Immediate email goes to the author and to `subscribed` rows; watchers without a row get none.
- Migration 114 adds nullable `notifications.subject_kind`, backfilled from `type` (and `subject_url` for mentions). Every `Notify*` call sets it. `repo_transfer` stays null.

## Acceptance criteria

- [ ] A muted user who also watches the repo gets nothing from a comment, review or state change.
- [ ] A subscribed user in a repo they don't watch (or `ignoring`) gets the in-app notification and the immediate email.
- [ ] A muted author is not notified; an unmuted author on a pre-feature thread still is.
- [ ] A mention reaches a muted user and flips the row to `subscribed`.
- [ ] `CanRead` and actor-silence rules still hold.
- [ ] Backfill migration test covers each type and a mention URL of each kind.
- [ ] `docs/notifications.md` updated: fan-out rules, email, `subject_kind`.
