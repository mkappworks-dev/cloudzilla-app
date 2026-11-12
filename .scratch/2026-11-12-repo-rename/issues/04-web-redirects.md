# Web and API redirects

Created: 2026-11-12
Category: enhancement
Status: done
Blocked by: 01

| Branch                      | Worktree                               | PR title                                        | Effort |
| --------------------------- | -------------------------------------- | ----------------------------------------------- | ------ |
| `feat/repo-rename-redirects` | `.worktrees/feat+repo-rename-redirects` | `feat(repos): redirect old repository paths` | M      |

## Problem

See [spec.md](../spec.md). Once a path has an alias, web and JSON API requests to it must reach the repo.

## Scope

- One router middleware before the `/{owner}/{repo}` routes (and `/api/repos/{owner}/{repo}`): when the exact path misses,
  call `ResolveAlias`; if the caller can read the target, redirect to the same sub-path and query under the new path.
- `GET`/`HEAD` → `301`; everything else → `308`; an HTMX GET gets `HX-Redirect`.
- A caller who can't read the target, an unknown alias and a soft-deleted target all get exactly the missing-repo reply.
- The exact live path always wins; the middleware runs only on a miss.
- `/{owner}` resolution (user, then org) and non-repo routes are untouched. Reserved owner names still apply.
- Handle sub-paths with encoded characters (`blob/main/a%20b.md`) and `*` wildcard routes unchanged.

## Files

- `internal/middleware/repo_alias.go` (new), `internal/router/router.go`, `internal/middleware/setup.go` (confirm no
  change needed; repo routes are post-setup), tests under `internal/router/`

## Acceptance criteria

- [x] `GET /acme/api/blob/main/README.md?plain=1` → `301` to `/acme/core/blob/main/README.md?plain=1`
- [x] `POST /api/repos/acme/api/issues` → `308`, and following it with the method creates the issue
- [x] Private repo: anonymous and unrelated users get the same status, headers and body as for a missing repo
- [x] A new live repo at the old name is served directly and the alias is gone (ticket 01)
- [x] Soft-deleted target does not redirect
- [x] No extra query on the live path (test with a query counter or by reading the code path)

## Verification

Router test with the throwaway `newVerificationRouter` harness (see memory/router-page-check-harness) for each row of
the spec's redirect table; compare private vs missing responses with `cmp`.

## Comments
