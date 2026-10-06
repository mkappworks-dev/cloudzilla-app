# Shared content-writable guard; close archived write gaps

Created: 2026-10-06
Category: bug
Status: done

Spec: [../spec.md](../spec.md#read-only-enforcement)

## What

Add `service.CheckContentWritable(repo *model.Repository) error`, backed by `Repository.ContentReadOnly()` so templates share the rule. It returns `ErrRepoArchived` for archived repos; ticket 07 extends it to pull mirrors. Every path that writes refs or commits to a repo's git storage calls it.

These paths call it but don't check today:

- branch and tag create and delete: `internal/handler/ref_handler.go:40,98,148,192`
- PR merge: `pull_handler.go:345-349`; auto-merge: `tryAutoMerge`, `pull_handler.go:449-453`
- apply suggestion: `pull_line_comment_handler.go:362`
- release create, which creates its tag: `release_service.go:82`
- changing the default branch: `RepoService.UpdateGeneral`, when `defaultBranch` changes. Description and website stay editable.

These existing checks switch to the guard: `git_http.go:114`, `ssh/server.go:219`, `repo_files_handler.go:198`, `profile_readme_handler.go:43`. `repo_service.go:1065` (template source) stays as is: it reads an archived template and doesn't write to it.

The UI hides the matching controls on archived repos: the `refs.templ` create and delete buttons, the merge box, apply suggestion, new release, and the default-branch field.

## Acceptance criteria

- [x] On an archived repo, every path above refuses the write: web forms with the existing archived message, JSON with 403 `{"error":"repository is archived"}`.
- [x] Auto-merge on an archived repo is skipped without error spam.
- [x] The write controls above are hidden on archived repos.
- [x] Tests cover each path, at handler or service level.
- [x] Ships as its own `fix(repo):` commit before any mirror code.

## Notes

`feat/web-file-edit` adds a web edit path. Whichever branch lands second wires the guard into it.

## Comments

**Claude, 2026-10-06:** Done.
- No UI applies suggestions today (it is API-only), so there was no control to hide.
- Enabling auto-merge is refused too, since it could never fire.
- Description and website stay editable on archived repos. Only a change to the default branch is refused.
- Wiki edits on archived repos are still allowed. The wiki is a separate git repo and stays writable on mirrors, so this ticket leaves it alone.
