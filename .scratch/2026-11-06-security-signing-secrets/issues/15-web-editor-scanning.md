# Scan and reject web editor commits

Created: 2026-11-13
Category: enhancement
Status: done
Blocked by: 13
Parallel with: 14, 16

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-17.3-web-editor-scanning` | `.worktrees/feat+phase-17.3-web-editor-scanning` | `feat(secrets): scan the web editor (17.3)` | S |

Spec: [spec-secret-scanning.md](../spec-secret-scanning.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/17-secret-scanning.md`.

## Problem

Daisy can paste a key into the web file editor, which does not go through receive-pack, so push protection never sees it.

## Scope

- Scan the new content in `CommitFile`, `editFile` and the new-file/edit/delete handlers (`repo_files_handler.go`) before the commit is made, through `PushScanService` with the same policy (closed alerts skip, only high confidence blocks, budget fail-open). A blocking finding returns 422 with the push-protection text and link, creates the alert as for a push, and makes no commit.
- Delete and rename carry no new content for a blob they only move; scan only changed content.
- The edit form shows the error where the existing 422 messages show and keeps the user's text.
- A fail-open web commit is scanned after the commit by the same post-receive step (`AfterWebCommit`).

## Files (hotspots in bold)

- **`internal/handler/repo_files_handler.go`**, `internal/service/code_service_files.go`, `internal/service/push_scan_service.go`
- `internal/view/pages/edit_file.templ`, `mockups/edit_file.html` (the error state)

## Acceptance criteria

- [x] A web edit adding a key is refused with 422 and the link; no commit exists; the form keeps the text
- [x] The same edit passes once its alert is closed as `used_in_tests`
- [x] A web edit that adds no secret is unchanged (existing handler tests)
- [x] Merges, squashes and wiki saves are untouched
- [x] Checked at desktop 1440 in **light and dark** on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`); evidence for both themes in the PR
- [x] The standing mocks in `mockups/` listed under Files are updated in the same PR (classes copied from the templ, `COVERAGE.md` / `index.html` / `shell.js` where affected) and checked under `make mockups` in both themes
- [ ] Verified on a throwaway server (scratch Postgres on a free port, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`) with native git; what was seen is in the PR
- [x] `make generate-templ` re-run and the `_templ.go` files committed (if `.templ` changed); `make lint` (`/Users/mk/go/bin/golangci-lint run`) and `go test ./...` pass

## Verification

Throwaway server: sign in, edit a file in the browser to add a key, see the error in light and dark at 1440, close the alert and save again.

## Comments

