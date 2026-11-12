# Transfer writes aliases

Created: 2026-11-12
Category: enhancement
Status: done
Blocked by: 02

| Branch                      | Worktree                               | PR title                                      | Effort |
| --------------------------- | -------------------------------------- | --------------------------------------------- | ------ |
| `feat/repo-rename-transfer` | `.worktrees/feat+repo-rename-transfer` | `feat(repos): redirect a transferred repo's old path` | S      |

## Problem

See [spec.md](../spec.md). `TransferRepo` changes `/{owner}/{repo}` and leaves nothing behind, so every old link 404s.

## Scope

- `TransferRepo` (direct move: to self or an owned org) and `AcceptTransfer` (to another user, on accept) insert an alias
  for the old `owner/name` in the same transaction as `UpdateOwner`/`transfers.Accept`, and delete any alias at the new
  path.
- Update the denormalized `events`/`notifications` owner text as ticket 02 does for names; share the helper.
- Transfer back to the previous owner drops the alias it would otherwise collide with.
- Rename and transfer stay separate: transfer never changes `name`.
- Transfer audit actions unchanged.

## Files

- `internal/service/repo_transfer.go`, `internal/store/repo_store.go` (`UpdateOwner`), `internal/store/repo_transfer_store.go`
  (`Accept`), shared helper from ticket 02

## Acceptance criteria

- [x] After a direct transfer, the old path redirects (web and git) to the new owner's path
- [x] After `AcceptTransfer`, the same; before accept, nothing changes
- [x] Failure of the DB step restores the dirs and leaves no alias
- [x] Transferring back works and leaves no self-alias
- [x] A pending transfer offered before a rename is still refused with `ErrTransferChanged`

## Verification

Integration tests; extend the existing transfer tests with the alias assertions.

## Comments
