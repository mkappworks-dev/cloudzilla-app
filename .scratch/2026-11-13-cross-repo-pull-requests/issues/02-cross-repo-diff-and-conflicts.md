# Diff, commits and conflicts across repositories

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent
Blocked by: 01

## Problem

See [spec.md](../spec.md). The Conversation, Commits and Files tabs, mergeability and CODEOWNERS reviewer requests work for a fork PR by resolving the head through its PR head ref.

## Acceptance criteria

- [ ] One function derives a PR's head ref (branch name, or `refs/pull/<n>/head`); callers of the `CodeService` pull methods use it.
- [ ] `resolveHead` and the merged-range code accept a full ref.
- [ ] Mergeability, ahead/behind, conflicts, commit list, diff and diff stats match the same-repo results for the same content.
- [ ] **Update branch** is not offered on a fork PR and the merge box says to update from the fork.
- [ ] Tests through the router: clean, conflicting and behind fork PRs.
