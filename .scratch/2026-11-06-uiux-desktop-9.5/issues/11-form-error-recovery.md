# Keep user input and report failures

Created: 2026-11-06
Category: bug
Status: done
Blocked by: 02, 06

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `fix/form-error-recovery` | `.worktrees/fix+form-error-recovery` | `fix(ui): keep typed content on errors and surface failed actions` | 2 | M | Edge +0.8 |

## Problem

New file returns bare `http.Error` pages and loses the typed content (`repo_files_handler.go:195-262`; `NewFileData` has no `Error`). Discussion title save, Lock and Mark answer reload regardless of status (`discussion_detail.templ:64,168,185,313,322`). Board `deleteCardJS`/`setProjectClosedJS` fail silently (`project_detail.templ:903-916`). Milestone edits leave header, breadcrumb and `<title>` stale (`milestone_detail.templ:23,84-90`). Webhook redeliver returns a bare 202 with no feedback.

## Scope

- New file re-renders with an `Alert` and the content, like edit file.
- Discussion and board actions show a toast on non-2xx and don't reload.
- Milestone PATCH swaps the header too (OOB swap).
- Redeliver shows a toast.

## Files

- `internal/handler/repo_files_handler.go`
- `internal/view/pages/new_file.templ`
- `internal/view/viewmodels_repo.go`
- `internal/view/pages/discussion_detail.templ`
- `internal/view/pages/project_detail.templ`
- `internal/handler/milestone_page_handler.go`
- `internal/view/fragments/webhook_deliveries.templ`

## Acceptance criteria

- [x] Each listed failure path shows a message and keeps input
- [x] Tests for new-file error re-render

## Verification

Force failures with a stubbed fetch / bad input on a throwaway server.

## Comments

2026-11-06 (triage): ready-for-agent.

2026-11-06 (wave 1 re-score, ticket 22): Related findings, not fixed in the polish PR: the file edit conflict path
asks users to copy their text and start over (`edit_file.templ:28`, Codex +0.3), and the diff-load failure on the PR
files page offers no retry (`pr_files.templ:40`; the polish PR only added "Reload the page to try again"). Ticket 27
reuses the keep-the-input mechanism.
