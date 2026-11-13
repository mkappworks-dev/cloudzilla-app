# Pushes to the fork update the pull request

Created: 2026-11-13
Category: enhancement
Status: done
Blocked by: 01

## Problem

See [spec.md](../spec.md). A push or force-push to the head branch of the fork updates the open PR in the base: objects copied, ref moved, `head_sha` updated, and the follow-ups that run on a same-repo head update run too.

## Acceptance criteria

- [x] `OnPostReceive` finds open PRs by `head_repo_id` and `head_branch` in any base repository, copies the new head and moves the ref.
- [x] A copy failure is logged and leaves the old ref and SHA; it never breaks the push.
- [x] Parity with a same-repo head update: that path only moves `head_sha` (reviews and checks read it), so the fork path does the same. `UpdateHeadSHAByBranch` now matches only same-repo PRs, so a push to a base branch that shares a fork PR's head branch name leaves the fork PR alone.
- [x] The base's quota is recomputed after the copy.
- [x] Service tests with real repositories: push, force-push, copy failure.
