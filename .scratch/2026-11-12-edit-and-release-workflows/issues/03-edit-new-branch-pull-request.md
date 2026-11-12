# Commit an edit to a new branch and open a pull request

Created: 2026-11-12
Category: enhancement
Status: ready-for-agent
Blocked by: 02

| Branch | Worktree | PR title | Effort | Score gain |
| --- | --- | --- | --- | --- |
| `feat/edit-new-branch-pull-request` | `.worktrees/feat+edit-new-branch-pull-request` | `feat(repo): commit a file edit to a new branch and open a pull request` | M | Features +0.2 |

Spec: [spec.md](../spec.md#commit-to-a-new-branch-and-open-a-pull-request).

## Problem

An edit always commits to the displayed branch. On a branch with `require_pull_request` it is refused with 422 and the
user must leave the page; elsewhere there is no way to propose a change instead of committing it.

## Scope

- Radio choice in the commit section: commit directly to `<branch>`, or create a new branch and open a pull request,
  with an editable branch name prefilled `<username>-patch-N`. The direct choice is disabled with its reason when the
  branch has `require_pull_request` (`BranchProtectionService.CheckWebCommit`), and the new-branch choice is preselected.
  The server keeps refusing a direct commit to a protected branch.
- `CodeService` creates the branch at the current tip of the edited branch and commits the edit onto it as one operation,
  returning the `RefUpdate`. A prefilled name that exists gets a counter; a typed name that exists is refused inline
  (422, `components.FieldError`). The stale-save check and ticket 02's conflict flow apply to both choices.
- After the commit run `PushService.AfterWebCommit` and redirect to `/{owner}/{repo}/pulls/new?head=<new>&base=<edited
  branch>` (303). Check that `PageNewPull` honours both query values and add them if it does not.
- Branch names go through the same validity check as other branch creation.

## Files

- `internal/handler/repo_files_handler.go`, `internal/view/pages/edit_file.templ`, `internal/view/viewmodels_repo.go`
- `internal/service/code_service_files.go`, `internal/handler/pull_page_handler.go` (prefill, if needed)
- `docs/code-browser.md`, `docs/pr-merge.md` if the PR form changes

## Acceptance criteria

- [ ] An edit can go to a new branch and lands on the prefilled pull request form
- [ ] On a protected branch the direct choice is disabled with the reason and the new-branch choice works
- [ ] A name collision is resolved (prefill) or refused inline (typed)
- [ ] The new branch is created from the current tip; a stale file still goes through the conflict flow
- [ ] Tests: service (branch + commit, collision, invalid name), handler (both choices, protected branch)
- [ ] Docs updated

## Mock updates

`mockups/edit_file.html`: commit-target radios, protected-branch state, name-collision error. Light and dark at 1440.

## Verification

Throwaway server per the harness: edit on `main` choosing a new branch, on a branch with `require_pull_request`, with a
colliding name. Follow the redirect to the PR form. Light and dark at 1440.
