# Pull-mirror sync

Created: 2026-10-06
Category: enhancement
Status: ready-for-agent
Blocked by: 02, 03

Spec: [../spec.md](../spec.md#pull-mirrors)

## What

`MirrorService.Sync(ctx, mirror) error` fetches upstream into the live bare repo:

- Refspecs are `+refs/heads/*:refs/heads/*` and `+refs/tags/*:refs/tags/*`, with `Prune: true`. `refs/pull/*` is never fetched.
- It calls `FetchContext` with the import guard in the context, honouring `mirror.allow_local_networks`. Extract a shared guard constructor from `import_guard.go` instead of copying it.
- It opens the storer through `gittransport.WrapForReceive`, so thin-pack deltas against objects already on disk resolve.
- When a token is set, it decrypts it with `secretbox` (purpose `"mirror-credential"`) and sends it as basic auth. If decryption fails, `last_error` reads "Stored credentials can't be decrypted; re-enter the token."
- It snapshots refs before and after the fetch and builds `[]*packp.Command`.
- When upstream HEAD moves, it moves the local HEAD and `default_branch`.
- Side effects, in this order:
  1. push webhooks per updated branch with an empty pusher name
  2. `OnPostReceive`
  3. `IndexRepo`
  4. `ParseAndStore`

  No activity events and no notifications.
- Errors map to user-facing messages, as `ImportService.failureMessage` does.

## Acceptance criteria

- [ ] Tests run against `testutil` git servers:
  - new commits
  - a force push
  - new and deleted branches and tags
  - a changed default branch
  - `refs/pull/*` ignored
- [ ] A test covers an incremental fetch where the server sends a thin pack whose bases exist only locally.
- [ ] A private-network URL is refused unless `allow_local_networks` is set.
- [ ] Webhooks fire with an empty pusher, and no rows are written to `events` or notifications.
- [ ] An unchanged upstream fires no side effects.
- [ ] The token never appears in errors or logs.
