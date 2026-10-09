# Web File Edit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The blob page's Edit and a new Delete work: a writer can edit, rename or delete a file on a branch from the browser, refused with 409 when the file changed after the page loaded.

**Architecture:** `CodeService` gains `GetBranchFile`, `EditFile` and `DeleteFile`, which share `CommitFile`'s path validation and commit-writing code and never create a branch. A dedicated edit page (modelled on New file) posts back to itself and re-renders with the user's input on a refusal. Delete is a `<dialog>` on the blob page that posts with htmx, answering `HX-Redirect` or a JSON error that `ConfirmDialog` turns into a toast.

**Tech Stack:** Go, chi v5, go-git v5, Templ, htmx 4.

**Spec:** [`.scratch/2026-10-06-web-file-edit/spec.md`](../../../.scratch/2026-10-06-web-file-edit/spec.md)

## Global Constraints

- Work in `/home/user/cloudzilla-app` on branch `feat/web-file-edit`.
- Integration tests need `TEST_DATABASE_DSN='postgres://cloudzilla:test@localhost:5433/cloudzilla_test?sslmode=disable'` (migrated). Without it DB tests skip silently, so always set it.
- Edit `.templ` files, then run `make generate-templ`. Never hand-edit `*_templ.go`. Don't run `templ fmt`.
- Comments only for a *why* the code can't show, one line by default.
- Commit with `git add <specific paths>`. Never stage `.claude/`. Conventional Commits subject, ending with only `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Before each commit: `go build ./...`, `go vet ./...`, `golangci-lint run ./...`, and the touched packages' tests with the DSN set.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/service/code_service_files.go` | `CleanFilePath` (extracted), `ErrFileChanged`, `ErrFileUnchanged`, `GetBranchFile`, `EditFile`, `DeleteFile`, tree insert/remove helpers, shared commit writer |
| `internal/service/code_service_tree.go` | `BlobResult.SHA`, `IsSymlink`, `IsBranch` |
| `internal/service/code_service_files_test.go` | service tests |
| `internal/handler/repo_files_handler.go` | `PageEditFile`, `SubmitEditFile`, `DeleteFile`; New file textarea → LF |
| `internal/handler/textarea.go` (new) | `textareaText`, `allCRLF` |
| `internal/handler/page_repo_handler.go` | `PageBlob` fills `CanEdit`/`CanDelete`/`EditURL`/`DeleteURL`/`BlobSHA` |
| `internal/handler/repo_files_edit_test.go` (new) | router-level tests |
| `internal/view/viewmodels_repo.go` | `BlobData` fields, `EditFileData` |
| `internal/view/pages/edit_file.templ` (new), `blob.templ` | edit page, Delete dialog |
| `internal/router/router.go` | `edit` and `delete` routes |
| `docs/code-browser.md`, `docs/access-control.md` | routes and rules |

---

## Task 1: Service — path validation, sentinels, `GetBranchFile`

- [ ] Extract `CleanFilePath(p) (string, error)` from `CommitFile` (trim `/`, `\`→`/`, 4096-byte cap, no empty/`.`/`..`/`.git` segments). `CommitFile` calls it.
- [ ] `ErrFileUnchanged = errors.New("file is unchanged")` replaces the inline error; `ErrFileChanged` (409).
- [ ] `GetBranchFile(owner, repo, branch, path, maxContent)` resolves the branch only (`ErrRefNotFound` otherwise) and returns SHA, mode, size, binary-ness and, for a regular file within `maxContent`, its content. Directories and submodules are `object.ErrFileNotFound`.
- [ ] `GetBlob` fills `SHA`, `IsSymlink`, `IsBranch`.

## Task 2: Service — `EditFile` and `DeleteFile` (test-first)

- [ ] Tests: edit in place keeps 100755; rename keeps the mode, creates folders, prunes emptied ones; rename onto an existing entry → `ErrPathCollision`; stale base SHA → `ErrFileChanged` with the branch unmoved; another file changed since → succeeds; unchanged path+content → `ErrFileUnchanged`; missing branch → `ErrRefNotFound` (never created); delete prunes folders and returns the nearest remaining folder; delete of a symlink works; delete of a submodule or with a stale SHA → `ErrFileChanged`.
- [ ] Implement with an `insertEntry` that takes a mode and whether an existing file may be replaced, a `removeEntry` that prunes empty trees, and a `commitOnto` writer shared with `CommitFile` (compare-and-swap through `gitref.Move`).

## Task 3: Handlers, routes, line endings (test-first)

- [ ] `textareaText(s, crlf)` turns CRLF into LF, then back into CRLF when the original used CRLF on every line (`allCRLF`). New file's textarea uses it; uploads don't.
- [ ] `GET/POST /{owner}/{repo}/edit/{ref}/*` behind `authMW` and `MaxBodySize(MaxEditFileBodyBytes)`; `POST /{owner}/{repo}/delete/{ref}/*` behind `authMW`.
- [ ] Access: 404 unreadable, 403 non-writer, 403 archived, 404 non-branch ref or missing file; 422 binary/symlink/over 1 MiB; 413 content over 1 MiB after normalisation.
- [ ] Refusals re-render the edit page with the user's path, content and message plus a banner, at the error's status. Delete answers JSON `{"error"}` or `redirectAfterSave` to the nearest folder.
- [ ] Router tests: edit commits LF and redirects; CRLF file stays CRLF; leading blank line survives; rename; stale SHA → 409 with form re-rendered; reader 403, anonymous redirect/401, archived 403, tag 404; binary/symlink/large 422; delete success HX-Redirect; delete stale → 409 JSON; author email follows keep-email-private; New file textarea commits LF.

## Task 4: Views

- [ ] `EditFileData` and `edit_file.templ`: path field, textarea rendering `"\n"+content`, hidden `blob_sha`, message, Cancel / Commit changes, error banner (`role="alert"`) with a link to the current version on a conflict.
- [ ] `blob.templ`: Edit shows when `CanEdit`; Delete (destructive outline) opens `blob-delete-dialog`, placed outside the `space-y` container, whose form `hx-post`s with `data-toast`.
- [ ] `make generate-templ`.

## Task 5: Docs, review, PR

- [ ] `docs/code-browser.md` section on editing and deleting; route rows in `docs/access-control.md`.
- [ ] Subagent review of the diff against the spec; fix findings.
- [ ] Tick the spec's acceptance criteria, `Status: done`; open the PR.
