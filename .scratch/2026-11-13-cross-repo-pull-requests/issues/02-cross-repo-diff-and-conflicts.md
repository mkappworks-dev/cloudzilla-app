# Diff, commits and conflicts across repositories

Created: 2026-11-13
Category: enhancement
Status: done
Blocked by: 01

## Problem

See [spec.md](../spec.md). The Conversation, Commits and Files tabs, mergeability and CODEOWNERS reviewer requests work for a fork PR by resolving the head through its PR head ref.

## Acceptance criteria

- [x] One function derives a PR's head ref (branch name, or `refs/pull/<n>/head`); callers of the `CodeService` pull methods use it.
- [x] `resolveHead` and the merged-range code accept a full ref.
- [x] Mergeability, ahead/behind, conflicts, commit list, diff and diff stats match the same-repo results for the same content.
- [x] **Update branch** is not offered on a fork PR and the merge box says to update from the fork.
- [x] Tests: clean and behind fork PRs read through the head ref at the service seam; a conflict takes the same path (`mergeFiles` on the resolved commits), so it has no case of its own.
