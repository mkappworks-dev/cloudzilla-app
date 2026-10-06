# Mirror scheduler loop

Created: 2026-10-06
Category: enhancement
Status: done
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

- [x] Against a local git server, a due mirror syncs once, and a mirror that isn't due isn't touched.
- [x] No more than `max_concurrent` syncs run at once.
- [x] A failing mirror records its error and backs off (the schedule itself is tested in the store). A success resets it.
- [x] With `mirror.enabled: false`, `main.go` doesn't start the loop.
- [x] Shutdown doesn't panic or leak the loop goroutine.

## Comments

**Claude, 2026-10-06:**
- Each lease is `mirror.timeout` plus one minute, so the result is recorded before another instance can claim the mirror.
- When a sync finishes it wakes the loop. A mirror that was due but didn't fit a slot then starts at once, instead of waiting up to 30s.
