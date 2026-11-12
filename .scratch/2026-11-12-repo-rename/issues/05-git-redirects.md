# Git transport redirects

Created: 2026-11-12
Category: enhancement
Status: done
Blocked by: 01

| Branch                     | Worktree                              | PR title                                          | Effort |
| -------------------------- | ------------------------------------- | ------------------------------------------------- | ------ |
| `feat/repo-rename-git`     | `.worktrees/feat+repo-rename-git`     | `feat(git): follow renamed repositories over HTTP and SSH` | M      |

## Problem

See [spec.md](../spec.md). Clones, fetches and pushes against a renamed repo's old URL must keep working.

## Scope

- HTTP (`internal/handler/git_http.go`): on a miss, `ResolveAlias`. `info/refs` (both services) → `301` to the new
  `info/refs` with the query kept; a bare `POST` to `git-upload-pack`/`git-receive-pack` → `308`.
- Order of checks per `docs/git-transport.md` is kept: everything before the repo lookup answers the same whether the
  repo exists or not. After the lookup, a caller who can't read the target gets the missing-repo reply (`401` challenge
  anonymous, `404` signed in), never a redirect.
- SSH (`internal/ssh/server.go`): resolve the alias after `Repo.Get` misses, authorize against the real repo (`canSee`,
  deploy-key binding by `repo.ID`), serve it, and write
  `warning: repository moved to <owner>/<name>; update your remote` to stderr before the transfer starts. Stdout carries
  only the pack protocol.
- Push through an old path works end to end: branch protection, quota, webhooks and activity use the real repo id.
- The log line for receive-pack carries the real owner and name.

## Files

- `internal/handler/git_http.go`, `internal/ssh/server.go`, tests beside `git_http_access_test.go` and
  `internal/ssh/server_test.go`, `docs/git-transport.md`

## Acceptance criteria

- [x] Native `git clone`, `fetch` and `push` over HTTP from the old URL succeed (git follows the `301`)
- [x] Native `git clone`, `fetch` and `push` over SSH from the old URL succeed and print the hint on stderr
- [x] `TestGitHTTP_PrivateRepo_LooksLikeMissingRepo` is extended: a private repo's alias looks like a missing repo on all
      three endpoints
- [x] A deploy key bound to the repo works through the alias; a deploy key for another repo still gets `repository not found`
- [x] A push through the old path to a protected branch is refused exactly as through the new path

## Verification

`internal/ssh/server_test.go` helpers and the `internal/testutil/gitserver.go` HTTP harness with real `git` (see
memory/e2e-git-push-harness); rename between two operations on the same clone.

## Comments
