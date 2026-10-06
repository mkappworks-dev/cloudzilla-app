# Web commits skip push side effects

Created: 2026-10-06
Category: bug
Status: needs-triage

## Problem

A push over HTTP or SSH fires push webhooks, records activity-feed events, ingests contributor stats, re-indexes code search and re-parses dependency manifests (`internal/handler/git_http.go` after receive-pack; `internal/ssh/server.go` repeats the same block). A commit made in the browser fires none of them. That covers New file, the profile README editor, and edit, rename and delete (`.scratch/web-file-edit/spec.md`).

So after a web commit, no external CI is triggered by the push webhook, the activity feed doesn't show the commit, code search keeps the old content, and contributor stats and the dependency graph go stale until the next real push.

## Fix sketch

- Move the post-receive block into one service method that takes the repo, the actor and the updated refs (old and new tip). HTTP and SSH receive-pack both call it.
- Call it after every web commit that moves a branch.

## Acceptance criteria

- [ ] A web commit fires the same push webhook, activity event, stats ingest, search re-index and dependency parse as a push of that commit.
- [ ] HTTP and SSH pushes behave as before, through the shared method.

## Comments

**Malith Kuruppu, 2026-10-06:** Split out of web-file-edit triage.
