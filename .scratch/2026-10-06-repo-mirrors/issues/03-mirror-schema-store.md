# Mirror schema, model, store and config

Created: 2026-10-06
Category: enhancement
Status: done

Spec: [../spec.md](../spec.md#data-model)

## What

- Migration `NNN_repo_mirrors.sql`, numbered at commit time. 102 is next today; check `origin/main` again before committing. It creates the `repo_mirrors` table as the spec's Data model describes, with an index on `next_sync_at`. A partial index can't help: the claim compares `lease_until` with `NOW()`, which an index predicate can't use.
- `model.RepoMirror`. `Repository.IsMirror` is derived with `EXISTS` in every `RepoStore` SELECT that scans a `Repository` (`repo_store.go:122,494,826` and any others).
- `MirrorStore` holds:
  - `Create`, `Get(repoID)`, `Update`, `Delete`
  - `ClaimDue(ctx, limit, lease) ([]RepoMirror, error)`, using the spec's `FOR UPDATE SKIP LOCKED` query and skipping archived and soft-deleted repos
  - `RecordSuccess` and `RecordFailure`, which computes backoff
  - `MarkDue(repoID)`
- It is wired into `Stores`.
- Config: the `mirror.*` block from the spec (`enabled`, `allow_local_networks`, `min_interval`, `default_interval`, `max_concurrent`, `timeout`) with its `CZ_MIRROR_*` env vars. `min_interval` must not exceed `default_interval`.

## Acceptance criteria

- [x] The migration applies cleanly. Migrations here are up-only.
- [x] `IsMirror` is true only for repos with a mirror row, everywhere a `Repository` is loaded.
- [x] Integration test: two concurrent `ClaimDue` calls never return the same row. An expired lease can be claimed again.
- [x] Backoff waits `interval × 2^(failures−1)`, capped at 24h but never below the interval, and success resets it.
- [x] The config defaults are 10m, 8h, 3 and 30m, and an invalid combination fails at startup.

## Comments

**Claude, 2026-10-06:**
- Backoff counts from the first failure, so one transient error keeps the normal schedule. A mirror with an interval over 24h never retries sooner than its interval.
- `feat/issue-closing-keywords` and a `claude/*` branch also added `102_*.sql`. They landed first, so the mirror migration is `107_repo_mirrors.sql`.
