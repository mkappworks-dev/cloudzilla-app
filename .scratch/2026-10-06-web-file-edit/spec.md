# Blob page's Edit button does nothing

Created: 2026-10-06
Category: bug
Status: done

## Problem

Writers see an Edit button next to Raw on a file page, but `PageBlob` sets `EditURL: "#"` (`internal/handler/repo_page_handler.go:601`), so the button goes nowhere. The only way to change an existing file from the browser is to retype its whole contents into New file at the same path. That replaces the file without checking whether anyone changed it in the meantime. A file can't be renamed or deleted from the browser at all.

## What exists

- **Blob page** (`internal/view/pages/blob.templ:89`) shows Edit when `CanWrite && !IsBinary`, so readers and anonymous viewers don't see it. It also shows on archived repos (the repo page's Add file menu checks `!IsArchived`, the blob page doesn't), on tag and commit-SHA refs, and on symlinks: go-git's `Tree.File` doesn't check the mode, so a symlink renders as a text file holding its target. A submodule's blob page 404s.
- **New file** (`GET`/`POST /{owner}/{repo}/new/{ref}/*`, `internal/handler/repo_files_handler.go`) commits through `CodeService.CommitFile` (`internal/service/code_service_files.go`), which already:
  - replaces an existing file and keeps its mode, so 100755 survives (`TestCommitFile_UpdatesFiles`, #139)
  - refuses a directory, symlink or submodule in the way with `ErrPathCollision` → 409 (#139)
  - refuses unchanged content (`file is unchanged` → 422)
  - caps paths at 4096 bytes and refuses `.git` segments (#140)
  - advances the branch with `gitref.Move`, a compare-and-swap on the tip it read, so a push that lands mid-write gives `ErrRefMoved` → 409 (#66). It reads the tip only at save time, though, so a push between page load and save gets overwritten without notice.
- **Body caps**: the route's `MaxBodySize(MaxNewFileBodyBytes)` (26 MB) and the global `MaxFormBodySize` ahead of CSRF (#142). An upload whose path ends in `/` keeps its own name (#141).
- **Branch protection**: git push (`CheckPushCommand`) enforces only `block_force_push`. `require_review_count` and `require_status_checks` gate PR merges (`CheckMerge`) and nothing else. New file doesn't consult protection at all, which matches git push, because a web commit is always a fast-forward.
- **Push side effects** (push webhooks, activity feed, contributor stats, search index, dependency parse) run after receive-pack in `internal/handler/git_http.go` and `internal/ssh/server.go`. New file and the profile README editor fire none of them.
- **Dialogs and toasts**: `components.ConfirmDialog` turns an htmx error's JSON `{"error": …}` into an error toast, and `data-toast` on a form shows a success toast across an `HX-Redirect`.

## Browser behaviour an editor must handle

Checked on 2026-10-06 in the desktop app's built-in browser against a throwaway echo server:

- A textarea submits every line break as CRLF, in urlencoded and multipart forms alike. `SubmitNewFile` stores `r.FormValue("content")` as it arrives, so a file typed into New file today is committed with CRLF line endings, and `bash deploy.sh` on a clone fails with `$'\r': command not found`. An editor that did the same would rewrite every line of an LF file.
- The HTML parser drops one newline directly after `<textarea>`. Unless the template emits a guard newline there, a file that starts with a blank line loses it.

## Design

### Edit and rename: a dedicated page

- The blob page's Edit links to `GET /{owner}/{repo}/edit/{ref}/{path}`, split with `SplitRefPath` like the other code-browser routes. The page is modelled on New file: an editable path field (changing it renames the file), a textarea prefilled with the blob, a commit message, and Cancel / Commit changes. The textarea renders `"\n" + content`, so the HTML parser's dropped newline is the guard's.
- The form carries the blob SHA the page loaded. `POST` to the same URL commits onto the branch in one commit and redirects (303) to the blob page at the new path.
- An empty commit message defaults to `Update <path>`, `Rename <old> to <new>`, or `Update and rename <old> to <new>`.
- When the save is refused, the edit page renders again with the user's text, path and message kept, and the reason in a banner. A 409 for a changed file links to the current version and tells the user to copy their changes before reloading.

### Delete: a dialog on the blob page

- Delete, next to Edit, opens a `<dialog>` with a commit message (default `Delete <path>`) and a Delete file button. The dialog's form carries the blob SHA and posts with htmx to `POST /{owner}/{repo}/delete/{ref}/{path}`.
- On success the server answers `HX-Redirect` to the nearest folder that still exists on the branch, or the repo root. Folders the deletion leaves empty are removed in the same commit. A refusal comes back as JSON `{"error": …}` and shows as an error toast.

### Rules

- **Stale saves**: the edit, rename or delete is refused with 409 unless the path on the branch still holds the blob SHA the page loaded. Commits to other files since then don't block it: a one-file change can't conflict with them, so it lands on the current tip. The `gitref.Move` compare-and-swap still catches a push mid-write (`ErrRefMoved` → 409).
- **Rename** refuses a target path that already holds anything (409), validates it as New file does (4096-byte cap, no `.git` segments, no `.`/`..`; 422), creates missing folders, prunes folders it empties, and keeps the file's mode.
- **Mode**: an edited or renamed file keeps its mode, so 100755 survives.
- **Line endings**: textarea CRLF goes back to LF unless the original file used CRLF on every line, which it keeps. A file mixing CRLF and LF comes back all LF. New file's textarea is normalised to LF too; uploads stay byte-for-byte.
- **Editable**: text files up to 1 MiB on a branch. Saves whose content exceeds 1 MiB after normalisation are refused with 413. Binary files, symlinks and larger files can't be edited (the blob page hides Edit; the edit page answers 422 saying why).
- **Deletable**: any regular or executable file, binary or large, and symlinks. Submodules and folders aren't (their blob page 404s).
- **Access**: viewers who can't read the repo get 404, readers without write access get 403, archived repos get 403, and refs that aren't branches (tags, commit SHAs, a branch deleted mid-edit) get 404. The blob page shows Edit and Delete only where they'd be accepted.
- **Unchanged**: an edit that changes neither path nor content is refused (422) instead of making an empty commit.
- **Protected branches**: web edits, renames and deletes follow git push's rules. `block_force_push` can't trigger, since each is a fast-forward.
- **Author**: the commit's author follows the user's keep-email-private setting, as New file's does.

## Acceptance criteria

- [x] A writer can open Edit from a text file on a branch, change it, and commit onto that branch with a message. The redirect lands on the updated blob.
- [x] Changing the path renames the file in the same commit: missing folders are created, emptied folders are pruned, the mode is kept, and an existing target is refused with 409.
- [x] Delete opens a dialog, commits the removal with the given or default message, prunes emptied folders and lands on the nearest remaining folder. Binary files, large files and symlinks can be deleted.
- [x] If the file changed on the branch after the page loaded, edit, rename and delete are refused with 409 and the branch doesn't move. A commit to another file in the meantime doesn't block them.
- [x] A refused edit re-renders the form with the user's text, path and message kept.
- [x] An executable file stays 100755 after an edit or rename.
- [x] Editing an LF file through the browser's textarea commits LF, a CRLF file stays CRLF, and a file that starts with a blank line keeps it. New file's textarea commits LF; uploads are unchanged.
- [x] The blob page hides Edit, and the edit routes refuse, for readers, anonymous viewers, archived repos, tag and SHA refs, binary files, symlinks and files over 1 MiB. Delete is hidden and refused for the same viewers, repos and refs.
- [x] An edit that changes neither path nor content is refused with a clear message.
- [x] The commit's author follows the keep-email-private setting.
- [x] `docs/code-browser.md` and the route table in `docs/access-control.md` cover the edit and delete routes.

## Relevant files

- `internal/handler/repo_page_handler.go` (`PageBlob`, `EditURL`)
- `internal/view/pages/blob.templ`, `internal/view/viewmodels_repo.go` (`BlobData`, `NewFileData`)
- `internal/handler/repo_files_handler.go` (`SubmitNewFile`, `parseNewFileForm`, body caps), `repo_files_page_handler.go` (`PageNewFile`)
- `internal/view/pages/new_file.templ`, `internal/view/components/confirm_dialog.templ` (toast wiring)
- `internal/service/code_service_files.go` (`CommitFile`, `insertBlobIntoTree`)
- `internal/service/code_service_tree.go` (`GetBlob`, `BlobResult`)
- `internal/service/code_service.go` (`resolveRef`, `ErrRefMoved`), `internal/gitref`
- `internal/router/router.go` (the `new` routes, `MaxFormBodySize`)
- Tests to reuse: `internal/handler/repo_files_handler_test.go`, `internal/service/code_service_files_test.go`, `internal/handler/branch_moved_test.go`
- `docs/code-browser.md`, `docs/access-control.md`

## Out of scope

- Fork-and-PR editing for users without write access.
- Push side effects for web commits: `.scratch/2026-10-06-web-commit-push-side-effects/spec.md`.
- A protection rule that requires a pull request, refusing direct pushes and web edits: `.scratch/2026-10-06-require-pull-request-rule/spec.md`.
- The profile README editor, which also commits textarea CRLF and has no stale-save check.

## Comments

**Malith Kuruppu, 2026-10-06 (triage):** Stale check on the file's blob SHA; editor cap 1 MiB; rename and delete in scope; New file's textarea normalised to LF in the same change; protected branches follow git push; push side effects and a require-PR rule are follow-ups; Delete removes any file or symlink. Layout: a dedicated edit page and a delete dialog on the blob page.
