# Web commits skip push side effects

Created: 2026-10-06
Category: bug
Status: done

## Problem

A push over HTTP or SSH fires push webhooks, records activity-feed events, ingests contributor stats, re-indexes code search and re-parses dependency manifests (`internal/handler/git_http.go` after receive-pack; `internal/ssh/server.go` repeats the same block). A commit made in the browser fires none of them. That covers New file, the profile README editor, and edit, rename and delete (`.scratch/web-file-edit/spec.md`).

So after a web commit, no external CI is triggered by the push webhook, the activity feed doesn't show the commit, code search keeps the old content, and contributor stats and the dependency graph go stale until the next real push.

## Fix sketch

- Move the post-receive block into one service method that takes the repo, the actor and the updated refs (old and new tip). HTTP and SSH receive-pack both call it.
- Call it after every web commit that moves a branch.

## Acceptance criteria

- [x] A web commit fires the same push webhook, activity event, stats ingest, search re-index and dependency parse as a push of that commit.
- [x] It also closes issues named by a closing keyword when it fast-forwards the default branch, as a push does.
- [x] A web commit to a pull request's head branch updates the PR's head SHA.
- [x] HTTP and SSH pushes behave as before, through the shared method.

## Scope

In: New file (including the first commit of an empty repo), edit, rename, delete, the profile README editor and applying a PR suggestion: every single-commit write `CodeService` makes to a branch for a signed-in user.

Out: PR merges (they fire `pull_request` and their own closing-keyword path; adding `push` there changes what integrations receive, so it gets its own spec) and wiki edits (a separate repo that none of these effects read).

## Tickets

- `issues/01-push-service.md`
- `issues/02-web-file-commits.md`
- `issues/03-readme-suggestion-docs.md`

## Comments

**Malith Kuruppu, 2026-10-06:** Split out of web-file-edit triage.

**Claude, 2026-10-08 (triage):** Gap confirmed: `git_http.go` and `ssh/server.go` carry the same post-receive block, and no `CodeService` write calls it. The block also runs `IssueCloser.CloseForPush` and, through `OnPostReceive`, updates PR head SHAs; both were missing from the criteria and are added, since "the same as a push" is the point. Scope above settles which web commits count; merges are the one call a maintainer may want to revisit.
