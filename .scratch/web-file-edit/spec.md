# Blob page's Edit button does nothing

Created: 2026-10-06
Category: bug
Status: needs-triage

## Problem

Writers see an Edit button next to Raw on a file page, but `PageBlob` sets `EditURL: "#"` (`internal/handler/page_repo_handler.go:601`), so the button goes nowhere. The only way to change an existing file from the browser is to retype its whole contents into New file at the same path. That replaces the file without checking whether anyone changed it in the meantime.

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

## Browser behaviour an editor must handle

Checked on 2026-10-06 in the desktop app's built-in browser against a throwaway echo server:

- A textarea submits every line break as CRLF, in urlencoded and multipart forms alike. `SubmitNewFile` stores `r.FormValue("content")` as it arrives, so a file typed into New file today is committed with CRLF line endings. An editor that did the same would rewrite every line of an LF file.
- The HTML parser drops one newline directly after `<textarea>`. Unless the template emits a guard newline there, a file that starts with a blank line loses it.

## Proposed behaviour

Pending triage of the open questions below.

- Edit links to `GET /{owner}/{repo}/edit/{ref}/{path}`: a page modelled on New file that shows the path, a textarea prefilled with the blob, a commit message that defaults to `Update <path>`, and Commit changes / Cancel.
- `POST` to the same URL commits onto the branch through `CommitFile` and redirects to the blob page.
- The form carries a base SHA. When the file has changed on the branch since the page loaded, the commit is refused with 409 instead of overwriting it.
- CRLF from the textarea is turned back into LF unless the original file used CRLF.
- The file keeps its mode.
- The edit page and its POST refuse, and the blob page hides Edit for, viewers without write access, archived repos, refs that aren't branches, binary files, symlinks, submodules and files over the editor's size cap.

## Acceptance criteria

- [ ] A writer can open Edit from a file on a branch, change the text, enter a commit message and commit onto that branch. The redirect lands on the updated blob.
- [ ] The commit's author follows the user's keep-email-private setting, as New file's does.
- [ ] If the file changed on the branch after the edit page loaded, the save is refused with 409 and the branch is left where it was.
- [ ] An executable file stays 100755 after an edit.
- [ ] Editing an LF file with the browser's textarea commits LF; a CRLF file stays CRLF. A file that starts with a blank line keeps it.
- [ ] The blob page hides Edit, and the edit page and POST refuse, for readers, anonymous viewers, archived repos, tag and SHA refs, binary files, symlinks and files over the cap. Submodules 404.
- [ ] Saving unchanged content is refused with a clear message instead of creating an empty commit.
- [ ] `docs/code-browser.md` lists the edit route and its rules.

## Relevant files

- `internal/handler/page_repo_handler.go` (`PageBlob`, `EditURL`)
- `internal/view/pages/blob.templ`, `internal/view/viewmodels_repo.go` (`BlobData`)
- `internal/handler/repo_files_handler.go` (`PageNewFile`, `SubmitNewFile`, `parseNewFileForm`, body caps)
- `internal/view/pages/new_file.templ`
- `internal/service/code_service_files.go` (`CommitFile`, `insertBlobIntoTree`)
- `internal/service/code_service_tree.go` (`GetBlob`, `BlobResult`)
- `internal/service/code_service.go` (`resolveRef`, `ErrRefMoved`), `internal/gitref`
- `internal/router/router.go` (the `new` routes, `MaxFormBodySize`)
- Tests to reuse: `internal/handler/repo_files_handler_test.go`, `internal/service/code_service_files_test.go`, `internal/handler/branch_moved_test.go`
- `docs/code-browser.md`

## Open questions

1. **Stale-edit check**: compare the file's blob SHA (refuse only when this file changed) or the branch's commit SHA (refuse when anything on the branch moved)?
2. **Size cap** for editing in the browser, given the blob page highlights up to 512 KiB and the profile README editor caps at 200 KB.
3. **Rename** (by changing the path) and **delete**: in scope?
4. **New file's CRLF**: fix it in the same change, since the same normalisation applies?
5. **Protected branches**: should a branch whose rule requires reviews refuse direct web edits, even though git push to it is allowed today?
6. **Push side effects**: should web commits fire webhooks, activity, search indexing and the rest, or is that a follow-up covering New file and the profile README too?
7. **Non-writers**: fork-and-PR editing is assumed out of scope.
8. **Layout**: a dedicated edit page, or editing inline on the blob page?
