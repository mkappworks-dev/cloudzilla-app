# Store the subject title on notifications

Created: 2026-10-09
Category: enhancement
Status: done

## What

Add `subject_title TEXT NOT NULL DEFAULT ''` (migration 111), `model.Notification.SubjectTitle`, and set it in each `Notify*` call that has the subject in hand (issue, PR, discussion). `NotifyMention` and `NotifyRepoTransfer` leave it empty.

## Acceptance criteria

- [x] Migration 111 adds the column; existing rows read back as `''`.
- [x] `NotificationStore.Create`, `ListByUser` and `ListUnreadReadable` write and read it.
- [x] Issue/PR/discussion notifications carry the subject's title.
- [x] `docs/notifications.md` mentions the field.
