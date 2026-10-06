# Mirror scheduler loop

Created: 2026-10-06
Category: enhancement
Status: ready-for-agent
Blocked by: 04

Spec: [../spec.md](../spec.md#scheduler)

## What

`MirrorService.Run(ctx)` is started from `cmd/server/main.go` on `workerCtx` when `mirror.enabled` is true.

- It wakes every 30s, or immediately through `Wake()`.
- Each pass claims due rows (`ClaimDue`, lease = `mirror.timeout`) and runs them under a `mirror.max_concurrent` semaphore.
- Each sync runs under `mirror.timeout`, in a context that shutdown does not cancel, as imports do.
- Afterwards it calls `RecordSuccess` or `RecordFailure`.
- `SyncNow(repoID)` = `MarkDue` + `Wake()`.

## Acceptance criteria

- [ ] With the clock and fetcher stubbed in tests, a due mirror syncs once, and a mirror that isn't due isn't touched.
- [ ] No more than `max_concurrent` syncs run at once.
- [ ] A failing mirror's `next_sync_at` backs off. A success resets it.
- [ ] With `mirror.enabled: false`, the loop doesn't start.
- [ ] Shutdown doesn't panic or leak the loop goroutine.
