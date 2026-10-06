# Never zero active superadmins

Created: 2026-10-06
Category: enhancement
Status: done

Spec: [../spec.md](../spec.md)

Blocked by: 01

## What to build

"Active superadmin" is `is_superadmin AND suspended_at IS NULL`, excluding the ghost. Add a store helper that, inside a transaction, locks every active superadmin row (`SELECT … FOR NO KEY UPDATE`, ordered by id) and reports whether removing the target leaves at least one. Suspend, demote and delete run it in the same transaction as their update and fail with `store.ErrLastSuperadmin`.

Self-service deletion (`UserService.DeleteUser` → `DeleteWithOwnedRepos`) runs the same check under the lock. `page_settings_handler.go` `DeleteAccount` shows `ErrLastSuperadmin` as "You are the only active superadmin. Promote another account first."

## Acceptance criteria

- [x] No sequence of actions, concurrent or not, and no self-service deletion leaves zero active superadmins.

## Tests

- Store: with one active superadmin, demote, suspend and delete each return `ErrLastSuperadmin`; with two, both concurrent demotions can't both succeed.
- Service: `DeleteUser` of the last active superadmin returns `ErrLastSuperadmin`.
