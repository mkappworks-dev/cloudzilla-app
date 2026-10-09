# Web file commits run the push side effects

Created: 2026-10-08
Category: bug
Status: done
Parent: ../spec.md

## What to build

`CodeService.CommitFile`, `EditFile` and `DeleteFile` return the branch's `RefUpdate` (branch, old tip, new tip). `PushService.AfterWebCommit` turns it into a push command and calls `AfterPush`. `SubmitNewFile`, `SubmitEditFile` and `DeleteFile` handlers call it after a successful commit.

## Acceptance criteria

- [x] New file, edit, rename and delete each fire the push webhook, record a push event, ingest contributor stats, re-index search and parse dependencies.
- [x] The first commit of an empty repo is a create (zero old tip).
- [x] A refused or failed commit fires nothing.
- [x] A web commit with `Fixes #N` on the default branch closes the issue.

## Blocked by

01
