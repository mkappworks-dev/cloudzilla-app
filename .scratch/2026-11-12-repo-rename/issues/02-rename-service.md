# Rename service, API and stored text

Created: 2026-11-12
Category: enhancement
Status: done
Blocked by: 01

| Branch                  | Worktree                           | PR title                                   | Effort |
| ----------------------- | ---------------------------------- | ------------------------------------------ | ------ |
| `feat/repo-rename-core` | `.worktrees/feat+repo-rename-core` | `feat(repos): rename a repository (core)` | L      |

## Problem

See [spec.md](../spec.md). Nothing can change a repo's name yet.

## Scope

- `RepoService.RenameRepo(ctx, repo, requesterID, newName)`: `CanManage`, `ValidateRepoName`, unchanged name is `422`,
  refuse with `409` while a mirror sync holds its lease or an import runs (the pattern stop-mirroring uses), then the
  `moveRepo` order: path and `NameHeld` checks, rename both dirs, one transaction, `revertDirs` on failure.
- Transaction: update `repositories.name`, insert the old-path alias, delete any alias at the new path, update
  `events` and `notifications` `owner_name`/`repo_name` and the `subject_url` prefix for the repo id (decision 7).
- Case-only rename: `moveRepo`'s `pathTaken` check must skip the repo's own dirs.
- `name` accepted on `PATCH /api/repos/{owner}/{repo}` (`UpdateRepo`, `repo_files_handler.go`); a request mixing `name`
  with other fields is `422`; `confirmGrant` step-up; `repo.rename` audit action (old and new name in the details).
- Errors: taken → `409`, invalid or reserved → `422`, forbidden → `403`/the existing missing-repo reply for unreadable.
- Pending transfer and `ErrTransferChanged` unchanged; add a test that accepting after a rename is refused.

## Files

- `internal/service/repo_rename.go` (new), `internal/service/repo_dirs.go`, `internal/store/repo_store.go` (rename
  query), `internal/store/event_store.go`, `internal/store/notification_store.go`, `internal/handler/repo_files_handler.go`,
  `internal/model/audit*.go`, `docs/api-reference.md`

## Acceptance criteria

- [x] Rename moves `<name>.git` and `<name>.wiki.git` and the row together; a failed transaction restores both dirs
- [x] The old path exists as an alias afterwards; renaming back drops it
- [x] Case-only rename works
- [x] Taken name, `.wiki` name, invalid name, unchanged name, mirror syncing and importing each return the stated status
- [x] Events and notifications of the repo show the new names; `events.payload` is untouched
- [x] An `admin` collaborator can rename, a `writer` can't; step-up is required
- [x] `repo.rename` audit entry written; webhook payloads after the rename carry the new `full_name` and the same `id`
- [x] Open PRs, forks, mirror row, webhooks and wiki survive (tests)

## Verification

Integration tests (`make test-integration`); `moveRepo` failure injection as in the transfer tests.

## Comments


Claude, 2026-11-12: "Importing" has no 409 here. An import holds no repository row until it publishes, so there is no
repo to rename while one runs; a pull-mirror import is covered by the mirror lease check once its row exists.
