# Refuse web commits

Created: 2026-10-08
Category: enhancement
Status: done
Blocked by: 01

New file, edit, rename and delete from the browser are refused with 422 when the branch matches a rule with `require_pull_request`.

## Acceptance criteria

- [x] `BranchProtectionService.CheckWebCommit(ctx, repoID, branch)` returns `ErrPushRequiresPR` for a flagged match and nil otherwise.
- [x] `SubmitNewFile`, `SubmitEditFile` and `DeleteFile` call it before committing and answer 422 naming the rule; the edit form keeps the user's text.
- [x] A branch the rule doesn't match still commits.
- [x] `docs/code-browser.md` notes the refusal.
