# Alias table and lifecycle rules

Created: 2026-11-12
Category: enhancement
Status: ready-for-agent
Blocked by: none

| Branch                  | Worktree                          | PR title                                            | Effort |
| ----------------------- | --------------------------------- | --------------------------------------------------- | ------ |
| `feat/repo-rename-alias` | `.worktrees/feat+repo-rename-alias` | `feat(repos): repo path aliases and lifecycle rules` | M      |

## Problem

See [spec.md](../spec.md). A renamed or transferred repo needs its old path remembered, with rules for when the memory
ends.

## Scope

- Migration `122_repo_aliases.sql` (re-check the next number after merging main): the table and indexes from the spec's
  Design section.
- `model.RepoAlias`; `store.RepoAliasStore` (`Add`, `Drop`, `Resolve`, `DropByRepo` as needed) wired into `Stores`
  (`internal/store/stores.go`); `RepoService.ResolveAlias`, wired into `Services`.
- `Resolve` joins `repositories` on `deleted_at IS NULL` and matches case-insensitively; it returns the repo, never
  another alias.
- Lifecycle (spec decisions 1 and 9): creating a repo (`claimRepo`), forking, importing and restoring consult the table
  and delete a conflicting alias in the same transaction as the insert; a restore whose old name is taken by a live
  repo or newer alias drops the alias and succeeds; soft-delete hides aliases (no change to the row); purge cascades.
- A name that is only an alias counts as taken by nobody: `ErrRepoNameTaken` applies to live repos and dirs only.

## Files

- `internal/db/migrations/122_repo_aliases.sql`, `internal/model/`, `internal/store/repo_alias_store.go`,
  `internal/store/stores.go`, `internal/service/services.go`, `internal/service/repo_dirs.go` (`claimRepo`),
  `internal/service/repo_service_lifecycle.go`, `internal/service/repo_service_fork.go`, import path in
  `internal/service/import_service.go`

## Acceptance criteria

- [ ] Migration applies on a populated DB and on a fresh one
- [ ] `Resolve` is case-insensitive and ignores aliases of soft-deleted repos
- [ ] Creating, forking, importing or restoring onto an alias name succeeds and removes the alias
- [ ] Restoring a repo whose old name is now held succeeds without the conflicting alias
- [ ] Purging a repo removes its aliases (cascade)
- [ ] Two aliases cannot share a path, case-insensitively
- [ ] No live-path lookup pays for the alias table (`RepoService.Get` unchanged)

## Verification

Integration tests against `TEST_DATABASE_DSN` (`make test-integration`) covering each lifecycle rule above.

## Comments
