# Wire CI for fork pull requests

Created: 2026-11-13
Category: enhancement
Status: done
Blocked by: 06, 07

## Problem

See [spec.md](../spec.md). A fork PR creates a `waiting` Run that a writer approves; the Run gets no secrets and a read-only job token bound to the fork so the runner can clone its commit.

## Acceptance criteria

- [x] `WorkflowRunService.OnPullRequest` is called with the head repository.
- [x] A fork Run's per-job token is bound to the fork repository.
- [x] Statuses a fork Run posts attach to the PR head SHA in the base.
- [x] The fork limit is removed from `docs/ci.md` "Known limitations".
- [x] End-to-end test with a real fork, at the service seam: open, waiting Run without secrets, approve, claim (clone URL and token on the fork), push to the fork, and a base push with a colliding branch name.
- [x] `Statuses a fork Run posts attach to the PR head SHA in the base` holds without a change: the Run belongs to the base, and its statuses are written against the base and the head SHA, which the base holds.
- [x] `WorkflowRunService.onBranchMoved` lists the pushed repository's open PRs and matches them by `HeadBranch` alone, so a push to a base branch named like a fork PR's head branch would create a bogus `synchronize` Run for it: skip PRs whose head is another repository.
