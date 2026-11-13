# Head repository or branch removed

Created: 2026-11-13
Category: enhancement
Status: done
Blocked by: 04

## Problem

See [spec.md](../spec.md). Deleting the head repository or its branch closes the open fork PRs with a timeline event; renames, transfers and a private head do not break them.

## Acceptance criteria

- [x] Soft and hard repository deletion close open PRs whose head is the repository, with a "head repository was deleted" event, before the row goes.
- [x] Deleting the head branch closes the PR with a timeline event.
- [x] The label snapshot keeps the origin visible after the head is gone; a merged PR keeps its tabs.
- [x] A renamed or transferred head repository leaves the PR working and shows the current name.
- [x] A reader who cannot read the head sees the PR and diff; the JSON withholds the name (`head_repo_hidden`). The words "a fork" in the pages are ticket 06's.

## Notes

- A hard delete of the head (purge, or its owner's account) sets `head_repo_id` to NULL without closing: those PRs stay open and still merge, because the base holds the objects. Soft delete, which every user delete does first, closes them.
