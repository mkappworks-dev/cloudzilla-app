# Inbox Unsubscribe mutes the thread

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent
Blocked by: 01, 02

## What

Per `mock.html` tab 2: `POST /api/notifications/unsubscribe` (row button and bulk bar) mutes the thread behind each selected notification, using `subject_kind` and `subject_id`, and marks that thread's unread notifications read so they leave the digest. Replace `NotificationService.UnsubscribeFromRepos` with `MuteThreads`. Drop `WatchedRepos` from the inbox view-model; the button is enabled on every row except `repo_transfer` and rows with a null `subject_kind`. Tooltip: "Unsubscribe: mute this thread. Repo watch unchanged."

## Acceptance criteria

- [ ] Muting writes `muted` for each distinct thread among the caller's own selected notifications and ignores other users' ids.
- [ ] Muting leaves the repo watch untouched.
- [ ] The thread's unread notifications (including ones not selected) become read and drop out of `ListUnreadForDigest`.
- [ ] The inbox re-renders on the caller's filter and page.
- [ ] `repo_transfer` and null-kind rows keep a disabled button with a tooltip.
- [ ] Service, store and render tests; `docs/notifications.md` documents the new meaning.
