# Pushes to the fork update the pull request

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent
Blocked by: 01

## Problem

See [spec.md](../spec.md). A push or force-push to the head branch of the fork updates the open PR in the base: objects copied, ref moved, `head_sha` updated, and the follow-ups that run on a same-repo head update run too.

## Acceptance criteria

- [ ] `OnPostReceive` finds open PRs by `head_repo_id` and `head_branch` in any base repository, copies the new head and moves the ref.
- [ ] A copy failure is logged and leaves the old ref and SHA; it never breaks the push.
- [ ] Review dismissal, status and auto-merge re-evaluation run as they do for a same-repo head update.
- [ ] The base's quota is recomputed after the copy.
- [ ] Service tests with real repositories: push, force-push, copy failure.
