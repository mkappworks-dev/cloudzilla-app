# Head repository column and create via API

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent
Blocked by: None

## Problem

See [spec.md](../spec.md). A PR can be created through the API with a head repository in the same fork tree. It stores the head repository, copies the head commit into the base and points `refs/pull/<n>/head` at it, and opens with the right `head_sha`.

## Acceptance criteria

- [ ] Migration adds `head_repo_id` and `head_repo_label`; existing rows are backfilled with `repo_id`.
- [ ] `POST /api/repos/{owner}/{repo}/pulls` accepts optional `head_repo` (`owner/name`); the PR JSON carries `head_repo` when it differs.
- [ ] Create needs `CanRead` on the base and `CanWrite` on the head repository and an existing head branch; a repository outside the base's fork tree is `422`; a mirror base is still refused.
- [ ] Head objects are copied into the base's store in pure go-git; `refs/pull/<n>/head` points at the head commit; `head_sha` is set.
- [ ] `refs/pull/*` is not advertised to clones or fetches over HTTP or SSH.
- [ ] Same-repo PRs behave exactly as before (no ref, no copy).
- [ ] Router/handler test through the API with a real fork; service tests for the fork-tree rule (sibling, fork of fork, unrelated).
