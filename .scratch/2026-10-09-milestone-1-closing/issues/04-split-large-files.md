# Split the largest source files

Created: 2026-10-09
Category: enhancement
Status: done

## Problem

Several files hold more than one concern, which makes review and conflict resolution harder. Sizes measured on 2026-10-08 (non-generated, non-test):

| File | Lines |
| ---- | ----- |
| `internal/service/repo_service.go` | 1231 |
| `internal/store/user_store.go` | 1057 |
| `internal/service/sso_service.go` | 1015 (LDAP, SAML and provisioning in one file) |
| `internal/handler/pull_page_handler.go` | 952 |
| `internal/handler/milestone_api_handler.go`, `internal/handler/milestone_page_handler.go` | 894 before the split (API and page handlers mixed) |

## Approach

Pure moves: cut whole functions into new files in the same package, named for the concern (for example `sso_ldap.go`, `sso_saml.go`, `sso_provision.go`; `milestone_api_handler.go` and `milestone_page_handler.go` following the `*_page_handler.go` convention in CLAUDE.md). No renames, no signature changes, no edits inside function bodies, no comment changes.

## Acceptance criteria

- [x] One PR per source file, so each is easy to review and revert.
- [x] `git diff --stat -M` shows moves only; the same set of top-level declarations exists before and after (compare `go doc -all` or a symbol list).
- [x] `go build ./...`, `go test ./...` with `TEST_DATABASE_DSN` set, and `golangci-lint run` unchanged and passing.
- [x] Update any doc or ticket that names the old file path (`grep -rn "sso_service.go" docs .scratch`).

## Blocked by

Sequence after open PRs that touch the same files have merged (several `feat/*` worktrees are active); check `git worktree list` and open PRs first.

## Comments

Claude, 2026-10-10: progress, one PR per file.

- Done: `internal/service/repo_service.go` (1271 lines), split into `repo_service.go` (types, constructor, `With*`, read queries) and `repo_service_{access,create,fork,settings,lifecycle,push}.go`.
- Done: `internal/store/user_store.go` (1057 lines), split into `user_store.go` (types, constructor, `userColumns`/`scanUser`, lookups, batch reads) and `user_store_{create,auth,reauth,totp,profile,delete,pins}.go`.
- Done: `internal/service/sso_service.go` (1024 lines), split into `sso_service.go` (type, constructor, config) and `sso_{ldap,saml,provision}.go`.
- Done: `internal/handler/milestone_handler.go` (894 lines), split into `milestone_api_handler.go` (JSON API handlers and their error helpers) and `milestone_page_handler.go` (page handlers, fragments and detail helpers).
- Done: `internal/handler/pull_page_handler.go` (952 lines), split into `pull_page_handler.go` (list, plus `pullInitials`, `pullFormatRelative` and the shared `firstNonEmpty`, `collaboratorUsernames`) and `pull_{new,detail,tabs}_page_handler.go` (new-PR form and reviewer options; detail; commits/checks/files tabs and the tab-badge helpers).
- All five files in the table are now split, so the ticket is done.

Claude, 2026-10-10: split `internal/handler/milestone_handler.go` into `milestone_api_handler.go` (JSON API handlers and their error helpers) and `milestone_page_handler.go` (page handlers, fragments and detail helpers); the old file is gone. Remaining: `user_store.go`, `pull_page_handler.go`; `sso_service.go` is in flight on `tech/split-sso-service`. Ticket stays open.
