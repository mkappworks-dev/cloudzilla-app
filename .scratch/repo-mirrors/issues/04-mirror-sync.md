# Pull-mirror sync

Created: 2026-10-06
Category: enhancement
Status: done
Blocked by: 02, 03

Spec: [../spec.md](../spec.md#pull-mirrors)

## What

`MirrorService.Sync(ctx, mirror) error` fetches upstream into the live bare repo:

- Refspecs are `+refs/heads/*:refs/heads/*` and `+refs/tags/*:refs/tags/*`, with `Prune: true`. `refs/pull/*` is never fetched.
- It calls `FetchContext` with the import guard in the context, honouring `mirror.allow_local_networks`. It reuses `importGuard` from `import_guard.go`, which already serves imports and, since #172, shares `dialPublic` with webhooks.
- It fetches into the repo's own storer. go-git never asks for thin packs, so the packfile fast path is safe.
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

- [x] Tests run against `testutil` git servers:
  - new commits
  - a force push
  - new and deleted branches and tags
  - a changed default branch
  - `refs/pull/*` ignored
- [x] A test covers an incremental fetch, and another fails if go-git starts requesting thin packs.
- [x] A private-network URL is refused unless `allow_local_networks` is set.
- [x] Webhooks fire with an empty pusher, and no rows are written to `events` or notifications.
- [x] An unchanged upstream fires no side effects.
- [x] The token never appears in errors or logs.

## Comments

**Claude, 2026-10-06:**
- The thin-pack risk in the spec was wrong. go-git's HTTP and SSH clients strip `thin-pack` from the server's capabilities (`transport.UnsupportedCapabilities`), so they never request one. Wrapping the storer would only have turned every fetched object into a loose file. `TestMirrorSync_GoGitNeverRequestsThinPacks` fails if go-git changes this.
- Side effects run inside the sync, not in background goroutines. The `mirror.max_concurrent` semaphore then bounds re-indexing load too.
