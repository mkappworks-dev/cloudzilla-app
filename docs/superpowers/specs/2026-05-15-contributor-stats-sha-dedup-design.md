# 2026-05-15 — Contributor stats: cross-push SHA dedup

**Status:** Design pending, not yet scheduled
**Branch convention:** `fix/contributor-stats-sha-dedup`
**Affected subsystem:** Stats ingestion (`internal/service/contributor_stats_service.go`, `internal/service/repo_service.go`, `internal/store/contributor_stats_store.go`)

---

## Problem

`OnPostReceive` walks every branch update and calls `ContributorStatsService.IngestCommit` for every commit reachable from the new tip (stopping at the old tip). With commit `9751ca4`, that ingest is now atomic — `contributor_week_stats` is upserted with an additive `commits = commits + 1` on conflict, so two concurrent pushes can no longer drop a write.

But the same commit SHA can still be counted **twice across pushes**, in three real scenarios:

1. **Force-push that rewrites history.** The pre-force tip's commits were ingested on push #1. After force-push, push #2 walks the new tip and (depending on the rewrite) re-walks ancestors that already landed in stats.
2. **Rebase + push.** A rebase keeps the original committer/author of each cherry-picked commit, but the SHA is different — so this case is not the bug. However, the *original* SHA still sits on a tracking branch elsewhere, and any later push that fast-forwards from a state including those originals re-counts them.
3. **Push of an already-pushed commit on a new branch.** When the user pushes `feature-b` whose history overlaps `feature-a`, the overlap region is re-walked. `OnPostReceive` stops at `cmd.Old` (the old tip of `feature-b`, which is zero for a new branch), so all of `feature-b`'s history is enumerated — including the commits already counted under `main` or `feature-a`.

The in-memory `seen map[plumbing.Hash]struct{}` in `OnPostReceive` ([repo_service.go:84](../../../internal/service/repo_service.go#L84)) only dedupes within a **single hook invocation**. Across invocations, no dedup exists. Contributor totals can drift upward over a repo's lifetime, and there is no log line to debug from because each individual ingest succeeds.

`commit_day_counts` (heatmap) has the same shape of problem but operates per-day so the visible blast radius is smaller; this design only addresses contributor stats. A follow-up could extend the table or reuse it.

### Why this needs a schema change

The right idempotency key is `(repo_id, sha)`, not `(repo_id, user_id, week)`. The week-bucket aggregate is intentionally aggregated — there is no way to ask, "have we already ingested this SHA?" from the existing table. Dedup needs a side table.

---

## Proposed design

### Schema (new migration)

```sql
-- 056_create_contributor_commits_ingested.sql
CREATE TABLE IF NOT EXISTS contributor_commits_ingested (
    repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    sha TEXT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    week DATE NOT NULL,
    additions INT NOT NULL DEFAULT 0,
    deletions INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (repo_id, sha)
);

CREATE INDEX IF NOT EXISTS idx_contributor_commits_repo_user_week
    ON contributor_commits_ingested (repo_id, user_id, week);
```

We store the resolved `user_id`, `week`, and per-commit `additions`/`deletions` so the row is self-contained for a future re-aggregation/backfill (see "Recovery" below).

### Store API

```go
// AttemptIngest atomically claims (repo_id, sha) for stats accounting.
// Returns true iff the row was newly inserted; false means this commit
// has already been counted and the caller must skip the aggregate update.
func (s *ContributorStatsStore) AttemptIngest(
    ctx context.Context,
    repoID int64, sha string, userID int64,
    week time.Time, additions, deletions int,
) (bool, error)
```

Implementation:

```sql
INSERT INTO contributor_commits_ingested
    (repo_id, sha, user_id, week, additions, deletions)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (repo_id, sha) DO NOTHING
RETURNING 1
```

A `sql.ErrNoRows` return means the conflict fired; the row already exists. Anything else is a real error.

### Service layer

`ContributorStatsService.IngestCommit` becomes:

```go
func (s *ContributorStatsService) IngestCommit(
    ctx context.Context,
    repoID, userID int64, when time.Time, sha string,
    additions, deletions int,
) error {
    week := store.MondayUTC(when)
    inserted, err := s.stats.AttemptIngest(ctx, repoID, sha, userID, week, additions, deletions)
    if err != nil {
        return err
    }
    if !inserted {
        return nil
    }
    return s.stats.AddDelta(ctx, repoID, userID, week, 1, additions, deletions)
}
```

Two writes per new commit (claim row + bucket increment) versus one today. The claim is the dedup gate; if it loses the race, the second writer skips the increment.

> **Atomicity note:** the two writes are not in a single transaction. If the process crashes between them, the SHA is claimed but the bucket increment is lost, permanently under-counting that one commit. Acceptable trade-off because (a) it is one commit, not a runaway drift, and (b) wrapping both in a transaction adds round-trip latency to every commit on the post-receive hot path. A reconciliation job (see Recovery) can correct any divergence.

### Caller change

`OnPostReceive` ([repo_service.go:142-167](../../../internal/service/repo_service.go#L142-L167)) passes `c.SHA` through:

```go
if err := s.contributorStats.IngestCommit(ctx, repo.ID, user.ID,
    c.AuthorTime, c.SHA, detail.TotalAdded, detail.TotalDeleted); err != nil {
    slog.Warn("post-receive: contributor stats ingest failed", ...)
}
```

The in-memory `seen` map in `OnPostReceive` can stay (it short-circuits before doing the `GetCommit` patch walk, which is more expensive than the dedup row insert), or be removed for simplicity.

### Migration / backfill

Existing rows in `contributor_week_stats` were populated without SHA tracking, so we cannot retroactively dedup them. Two options:

1. **Leave existing data as-is.** New commits dedup from migration day forward; old aggregates may already be over-counted but stable. Documented as known drift.
2. **Reset and re-walk.** Truncate `contributor_week_stats`, then for each repo walk `default_branch` from root and re-ingest. Expensive (full git walk per repo) and changes visible numbers on the contributors page. Could be a one-shot CLI: `cloudzilla stats backfill-contributors`.

Recommend option 1 for ship; expose option 2 as an admin CLI for repos where drift is reported.

### Recovery / reconciliation

A future cron job (out of scope here) could detect drift by re-summing `contributor_commits_ingested.additions/deletions` GROUP BY `(repo_id, user_id, week)` and comparing against `contributor_week_stats`. Mismatches are evidence of the crash-between-writes case above; a single corrective UPDATE realigns them. Storing per-commit `additions`/`deletions` in the dedup table is what makes this possible — without it, the table would need a separate audit log.

---

## Affected files

- `internal/db/migrations/056_create_contributor_commits_ingested.sql` (new)
- `internal/store/contributor_stats_store.go` — add `AttemptIngest`
- `internal/store/contributor_stats_store_test.go` — add test covering double-insert returns `(false, nil)`
- `internal/service/contributor_stats_service.go` — `IngestCommit` signature gains `sha string`; gate increment on claim
- `internal/service/repo_service.go` — pass `c.SHA` through; optionally drop in-memory `seen` map
- `cmd/cloudzilla/` — optional `stats backfill-contributors` CLI for retroactive correction

## Risk / blast radius

- **Hot path cost:** one extra write per commit on push. For a 500-commit push: 500 INSERTs (mostly hitting `ON CONFLICT DO NOTHING` on resends). Within the existing per-commit `GetCommit` patch-walk cost, which is significantly more expensive.
- **Storage:** one row per (repo, commit). A repo with 100k commits uses ~10 MB in the dedup table (~100 bytes per row counting indexes). Bounded by commit count, which the system already accepts.
- **Backwards compat:** existing aggregates remain readable; the contributors page does not change. Only future ingests are deduped.

## Not in scope

- Heatmap (`commit_day_counts`) SHA-level dedup — same shape, separate change.
- Backfilling historical drift on production repos. Provide CLI; do not auto-run.
- Reconciliation cron job — design only mentions the table layout that would support it.

---

## Open questions

1. Do we want the in-memory `seen` map preserved as a cheap pre-filter, or removed for simplicity?
2. Should the dedup table FK to `repositories.id` with `ON DELETE CASCADE` (assumed above) or with `SET NULL` / no FK to allow stats survival across repo deletes? Decide against the existing semantics of `contributor_week_stats`, which already cascades.
3. Is the two-write non-transactional gap acceptable, or do we wrap in a `BEGIN` / `COMMIT`? Recommendation: leave non-transactional; rely on the reconciliation path for the rare crash window.
