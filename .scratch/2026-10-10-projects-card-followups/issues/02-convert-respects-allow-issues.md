# Convert to issue must respect allow_issues

Created: 2026-10-10
Category: bug
Status: done

## Problem

`POST /api/repos/{owner}/{repo}/projects/{id}/cards/{cardID}/convert` creates an issue even when the repository has issues disabled. `CreateIssue` answers 404 `not found` for that repository, and the card modal still offers "Convert to issue".

## Scope

- `ProjectService.ConvertCardToIssue` returns `ErrIssuesDisabled` after the `CanWrite` check and before any state change; `writeProjectError` maps it to 404 `not found`, the reply `CreateIssue` gives. Putting it in the service covers every caller.
- The card modal renders the Convert button and its confirmation only when `data.Repo.AllowIssues`; Delete and the read-only footer are unchanged.
- `docs/api-reference.md` and `docs/access-control.md` list the new error.

## Acceptance criteria

- [x] Converting in a repo with `allow_issues = false` returns the status and body issue creation returns, creates no issue and leaves the card unlinked
- [x] The happy path still converts (also after re-enabling issues)
- [x] The Convert button is absent from the card dialog when issues are disabled and present when enabled
- [x] API and access-control docs updated
