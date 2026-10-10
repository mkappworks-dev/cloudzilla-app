# Split the largest source files

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent

## Problem

Several files hold more than one concern, which makes review and conflict resolution harder. Sizes measured on 2026-10-08 (non-generated, non-test):

| File | Lines |
| ---- | ----- |
| `internal/service/repo_service.go` | 1231 |
| `internal/store/user_store.go` | 1057 |
| `internal/service/sso_service.go` | 1015 (LDAP, SAML and provisioning in one file) |
| `internal/handler/page_pull_handler.go` | 952 |
| `internal/handler/milestone_api_handler.go`, `internal/handler/milestone_page_handler.go` | 894 before the split (API and page handlers mixed) |

## Approach

Pure moves: cut whole functions into new files in the same package, named for the concern (for example `sso_ldap.go`, `sso_saml.go`, `sso_provision.go`; `milestone_api_handler.go` and `milestone_page_handler.go` following the `page_*_handler.go` convention in CLAUDE.md). No renames, no signature changes, no edits inside function bodies, no comment changes.

## Acceptance criteria

- [ ] One PR per source file, so each is easy to review and revert.
- [ ] `git diff --stat -M` shows moves only; the same set of top-level declarations exists before and after (compare `go doc -all` or a symbol list).
- [ ] `go build ./...`, `go test ./...` with `TEST_DATABASE_DSN` set, and `golangci-lint run` unchanged and passing.
- [ ] Update any doc or ticket that names the old file path (`grep -rn "sso_service.go" docs .scratch`).

## Blocked by

Sequence after open PRs that touch the same files have merged (several `feat/*` worktrees are active); check `git worktree list` and open PRs first.

## Comments

Claude, 2026-10-10: progress, one PR per file.

- Done: `internal/service/repo_service.go` (1271 lines), split into `repo_service.go` (types, constructor, `With*`, read queries) and `repo_service_{access,create,fork,settings,lifecycle,push}.go`.
- Remaining: `user_store.go`, `page_pull_handler.go`. `sso_service.go` and `milestone_handler.go` are in flight on `tech/split-sso-service` and `tech/split-milestone-handler`.
- The ticket stays open until every file in the table is split.

Claude, 2026-10-10: split `internal/handler/milestone_handler.go` into `milestone_api_handler.go` (JSON API handlers and their error helpers) and `milestone_page_handler.go` (page handlers, fragments and detail helpers); the old file is gone. Remaining: `user_store.go`, `page_pull_handler.go`; `sso_service.go` is in flight on `tech/split-sso-service`. Ticket stays open.
