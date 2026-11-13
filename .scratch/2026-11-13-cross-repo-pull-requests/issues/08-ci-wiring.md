# Wire CI for fork pull requests

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent
Blocked by: 06, 07

## Problem

See [spec.md](../spec.md). A fork PR creates a `waiting` Run that a writer approves; the Run gets no secrets and a read-only job token bound to the fork so the runner can clone its commit.

## Acceptance criteria

- [ ] `WorkflowRunService.OnPullRequest` is called with the head repository.
- [ ] A fork Run's per-job token is bound to the fork repository.
- [ ] Statuses a fork Run posts attach to the PR head SHA in the base.
- [ ] The fork limit is removed from `docs/ci.md` "Known limitations".
- [ ] End-to-end test with a real fork.
