# Clarify handler file naming

Created: 2026-10-10
Category: enhancement
Status: done

## Problem

The `page_*_handler.go` → `*_page_handler.go` rename left the handler folder with a convention that the folder itself contradicts, plus a few names that mislead.

- **CLAUDE.md says "full pages go in `*_page_handler.go`", but most page handlers don't.** 31 non-test files in `internal/handler/` define `Page*` methods without the suffix (`gist_handler.go`, `discussion_handler.go`, `project_handler.go`, `wiki_handler.go`, `explore_handler.go`, `signup_handler.go`, …). Only a concern that also has JSON handlers got a separate `<concern>_page_handler.go`.
- **`page_handler.go` is not a page handler.** It holds `PageHome` and its two filter/sort helpers (`applyHomeRepoFilter`, `applyHomeRepoSort`), plus the helpers every page uses: `basePage`, `readableRepo`, `readableRepoPage`, `withRepoSubnav`, `repoSubnavCounts`, `withAccountSubnav`, `accountCounts`. It is the only `page_*` file left that isn't a page. Its tests are `page_helpers_test.go` and `page_code_browser_test.go`.
- **Two files named `viewmodels.go`.** `internal/handler/viewmodels.go` is a block of type aliases re-exporting `internal/view`, while `internal/view/viewmodels*.go` holds the real view-models CLAUDE.md points to. Its header comment also says the definitions "now live" elsewhere.
- **`internal/service/code_service_tree_lc.go`** — `lc` is "last commit" (`TreeEntryWithLastCommit`, `ListEntriesWithLastCommit`).

## Scope

Pure moves and renames: no signature changes, no edits inside function bodies, no comment changes beyond the one named below. One PR, a handful of files, because several in-flight branches touch `internal/handler/`.

1. **CLAUDE.md wording.** Replace "full pages go in `*_page_handler.go`" with the rule the folder follows. Count first (`grep -lE '^func \(h \*Handler\) Page' internal/handler/*.go | grep -v _test`) and write the rule that matches. The likely wording: pages share `<concern>_handler.go` unless that file holds JSON API handlers, in which case they go in `<concern>_page_handler.go`. The same sentence appears in `.scratch/2026-10-09-cz-cli/issues/07b-device-approval-page.md`.
2. **Split `page_handler.go`.** `PageHome`, `applyHomeRepoFilter` and `applyHomeRepoSort` go to `home_page_handler.go`. `basePage`, `readableRepo`, `readableRepoPage`, `withRepoSubnav`, `repoSubnavCounts`, `withAccountSubnav` and `accountCounts` go to a file named for what they are (for example `base_page.go`). Rename `page_helpers_test.go` and `page_code_browser_test.go` to follow whatever they test.
3. **Rename `internal/handler/viewmodels.go`** to say it holds aliases (for example `view_aliases.go`), and fix its header so it doesn't say the definitions "now" live elsewhere.
4. **Rename `code_service_tree_lc.go`** to `code_service_tree_last_commit.go`.
5. **Update every doc, spec and ticket path** that names a moved file: `grep -rnE 'page_handler|page_helpers_test|page_code_browser_test|viewmodels\.go|tree_lc' docs .scratch CLAUDE.md CONTRIBUTING.md`. `docs/ROADMAP.md` lines naming `page_handler.go` `PageCommit` / `PagePullDetail` were already stale after earlier splits; point them at the file that holds the symbol today.

## Out of scope

- Renaming the other 31 page-method files to `*_page_handler.go`. It is large churn and collides with every open feature branch.
- Renaming scenario-named tests (`archived_writes_test.go`, `private_issue_lists_test.go`, …).
- `fake_google.go`. It sits outside `_test.go` on purpose (router tests use it; the comment says why).

## Acceptance criteria

- [x] CLAUDE.md states the page-handler convention the folder actually follows
- [x] `page_handler.go` no longer exists; its symbols moved to files named for their concern
- [x] `internal/handler/viewmodels.go` and `code_service_tree_lc.go` renamed
- [x] `git diff --stat -M` shows only renames, whole-function moves, and doc path edits
- [x] No stale paths in `docs`, `.scratch`, `CLAUDE.md`, `CONTRIBUTING.md`
- [x] `go build ./...`, `go vet ./internal/handler/...`, `golangci-lint run` and `go test ./...` pass

## Blocked by

PR `tech/rename-page-handlers` merging, so the follow-up branches from `origin/main` instead of stacking.
