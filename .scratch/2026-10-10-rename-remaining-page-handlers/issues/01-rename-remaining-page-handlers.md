# Rename remaining page handlers to `*_page_handler.go`

Created: 2026-10-10
Category: enhancement
Status: done

## Problem

`tech: clarify handler file naming` settled on "pages share `<concern>_handler.go` unless that file holds JSON API handlers". That leaves two places to look for a page handler, and 29 non-test files in `internal/handler/` still define `Page*` methods without the `_page_handler.go` suffix. Every page handler in `*_page_handler.go` sorts next to its JSON `<concern>_handler.go` and makes `ls internal/handler | grep _page_handler` the full list of pages.

## Scope

Pure `git mv` renames and whole-function moves: no signature changes, no edits inside function bodies, no comment changes beyond the rule sentence named below.

1. **Rename the 12 page-only files** to `<concern>_page_handler.go`: `account`, `activity`, `audit`, `code_search`, `dependency`, `explore`, `insights`, `invite`, `register`, `search`, `setup`, `signup`. Rename `activity_handler_test.go`, `invite_handler_test.go`, `register_handler_test.go`, `setup_handler_test.go` and `signup_handler_test.go` with them.
2. **Split the mixed files.** Move each `Page*` method, and the unexported helpers and constants only those methods use, into `<concern>_page_handler.go`. Form `POST` handlers, JSON handlers and helpers shared with them stay in `<concern>_handler.go`. Files: `admin`, `admin_user`, `email_verification`, `discussion`, `gist`, `oauth_app`, `password_reset`, `release`, `repo`, `repo_files`, `repo_import`, `star`, `sso`, `topic`, `totp`, `wiki`.
3. **`project_handler.go` is deferred.** Open PR #240 (`feat/projects-github-style`) rewrites it, so splitting it now would conflict. Do it after #240 merges.
4. **Restate the CLAUDE.md rule** (step 5 of "Adding a feature") and the same sentence in `.scratch/2026-10-09-cz-cli/issues/07b-device-approval-page.md`: every `Page*` method lives in `<concern>_page_handler.go`.
5. **Update every doc, spec and ticket path** that names a moved file or a moved symbol's file.

## Out of scope

- `project_handler.go` (see 3).
- Renaming scenario-named tests (`account_delete_test.go`, `repo_page_access_test.go`, …).

## Acceptance criteria

- [x] The 12 page-only files and their five tests are renamed with `git mv`
- [x] Every `Page*` method outside `project_handler.go` lives in a `*_page_handler.go` file
- [x] CLAUDE.md and the 07b ticket state the rule the folder follows
- [x] `git diff --stat -M` shows only renames, whole-function moves, and doc path edits
- [x] No stale paths in `docs`, `.scratch`, `CLAUDE.md`, `CONTRIBUTING.md`
- [x] `go build ./...`, `go vet ./internal/handler/...`, `golangci-lint run` and `go test ./...` pass

## Blocked by

None. PR #241 and `tech: clarify handler file naming` (#243) have merged.

## Comments

Claude, 2026-10-10: Done except `project_handler.go`, which still defines `PageProjects` and `PageProjectDetail`. Split it into `project_page_handler.go` once #240 (`feat/projects-github-style`) has merged. The 12 page-only files and 16 mixed files are done.
