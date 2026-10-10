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
| `internal/handler/milestone_handler.go` | 894 (API and page handlers mixed) |

## Approach

Pure moves: cut whole functions into new files in the same package, named for the concern (for example `sso_ldap.go`, `sso_saml.go`, `sso_provision.go`; `milestone_api_handler.go` and `page_milestone_handler.go` following the `page_*_handler.go` convention in CLAUDE.md). No renames, no signature changes, no edits inside function bodies, no comment changes.

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
- Done: `internal/store/user_store.go` (1057 lines), split into `user_store.go` (types, constructor, `userColumns`/`scanUser`, lookups, batch reads) and `user_store_{create,auth,reauth,totp,profile,delete,pins}.go`.
- Done: `internal/service/sso_service.go` (1024 lines), split into `sso_service.go` (type, constructor, config) and `sso_{ldap,saml,provision}.go`.
- Remaining: `page_pull_handler.go`. `milestone_handler.go` is in flight on `tech/split-milestone-handler`.
- The ticket stays open until every file in the table is split.
