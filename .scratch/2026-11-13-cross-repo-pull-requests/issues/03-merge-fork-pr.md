# Merge a fork pull request

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent
Blocked by: 02

## Problem

See [spec.md](../spec.md). Fast-forward, merge commit and squash work on a fork PR, write only to the base repository, and leave the head repository untouched.

## Acceptance criteria

- [ ] Each merge method merges a fork PR and the base branch tip is correct; the fork's refs are unchanged.
- [ ] The "head moved since checked" guard compares the head ref's current SHA and returns `409`.
- [ ] Branch protection, required reviews and checks, auto-merge and closing keywords apply.
- [ ] **Delete branch after merge** is offered for same-repo PRs only.
- [ ] `merged_base_sha` and `merged_head_sha` are recorded so the Commits and Files tabs survive the fork's deletion.
