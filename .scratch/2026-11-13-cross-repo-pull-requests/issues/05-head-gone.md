# Head repository or branch removed

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent
Blocked by: 04

## Problem

See [spec.md](../spec.md). Deleting the head repository or its branch closes the open fork PRs with a timeline event; renames, transfers and a private head do not break them.

## Acceptance criteria

- [ ] Soft and hard repository deletion close open PRs whose head is the repository, with a "head repository was deleted" event, before the row goes.
- [ ] Deleting the head branch closes the PR with a timeline event.
- [ ] The label snapshot keeps the origin visible after the head is gone; a merged PR keeps its tabs.
- [ ] A renamed or transferred head repository leaves the PR working and shows the current name.
- [ ] A reader who cannot read the head sees the PR and diff with the head shown as "a fork".
