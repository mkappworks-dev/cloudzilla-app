# 2026-05-22 — Stats hardening + related fixes

**Status:** Implemented. Migration shipped as `073_create_commits_ingested.sql`, not 066; see the plan's "As shipped" notes.
**Branch:** `fix/contributor-stats-sha-dedup` (all six workstreams land on this single branch, by user decision)
**Affected subsystems:** stats ingestion (`internal/service/contributor_stats_service.go`, `commit_stats_service.go`, `repo_service.go`, `internal/store/contributor_stats_store.go`, `commit_stats_store.go`), admin CLI (`cmd/cloudzilla/`), releases (`internal/service/release_service.go`), pull-request store tests, test infrastructure.

---

## Background

The branch already shipped per-SHA dedup for contributor week-stats (`contributor_commits_ingested` table, `IngestCommit` gated on an `INSERT ... ON CONFLICT DO NOTHING` claim — see [`2026-05-15-contributor-stats-sha-dedup-design.md`](./2026-05-15-contributor-stats-sha-dedup-design.md)). That work left documented gaps and out-of-scope follow-ups. This spec addresses all of them, plus two pre-existing unrelated bugs surfaced while running the integration suite.

## Locked decisions

1. **Crash-window:** the claim + aggregate writes are wrapped in a single DB transaction. This eliminates the non-transactional gap entirely, so a separate reconciliation job is **not** built.
2. **Heatmap dedup:** the heatmap (`commit_day_counts`) shares **one** `(repo_id, sha)` claim with contributor stats. The dedup table is renamed to the generic `commits_ingested`.
3. **Backfill:** delivered as a CLI command, **dry-run by default**, `--apply` to write, `--repo` / `--all` scope.

---

## Section A — Transactional unified per-commit ingest

### Problem

`OnPostReceive` ([repo_service.go](../../../internal/service/repo_service.go)) has two unrelated ingestion paths:

- a **batch** call `commitStats.Ingest(ctx, repoID, samples)` for the heatmap (`commit_day_counts`), and
- a **per-commit loop** calling `contributorStats.IngestCommit(...)` for week-stats.

Only the per-commit loop has SHA dedup. The heatmap path can still double-count across pushes. The contributor path's claim and aggregate increment are two separate autocommit writes — a crash between them permanently under-counts one commit.

### Design

Introduce a single ingestion entry point that handles one commit end-to-end inside one transaction:

```
IngestCommit(ctx, repoID, userID int64, when time.Time, sha string, additions, deletions int) error
```

Within one `sql.Tx` it:

1. Claims `(repo_id, sha)` in `commits_ingested` via `INSERT ... ON CONFLICT DO NOTHING RETURNING 1`.
2. If the claim is lost (already ingested) → commit the empty transaction, return `nil`.
3. If claimed → increment the contributor week-stat (`contributor_week_stats`, additive) **and** the heatmap day-count (`commit_day_counts`, additive), then commit.

Because all three writes share one transaction, a crash leaves either everything or nothing — no divergence.

### Store changes — one transactional ingest method

A new `ContributorStatsStore.IngestCommitTx` owns the transaction (consistent with the codebase's transaction-in-store pattern, e.g. `gist_store.go`). It opens one `sql.Tx`, performs the claim, the week-stat increment, and the day-count increment, then commits. To keep the SQL DRY, the core of each write is extracted into an unexported helper accepting a small unexported `dbtx` interface (satisfied by both `*sql.DB` and `*sql.Tx`); the existing public `AddDelta` / `AddCount` keep their signatures and autocommit behavior as thin wrappers for their other callers. The standalone public `AttemptIngest` from the prior task is removed — its logic moves into the unexported helper and `IngestCommitTx`, which also resolves the parameter-ordering footgun flagged in review.

### Caller change

`OnPostReceive`'s per-commit loop calls the unified `IngestCommit` once per commit; `IngestCommit` delegates to `IngestCommitTx`. The batch heatmap call (`commitStats.Ingest`) is removed from `OnPostReceive`. `CommitStatsService.Ingest` itself is **retained** — `commit_stats_backfill.go`'s `BackfillRecentCommits` still uses it.

### Fallback

If unifying the heatmap into the per-commit transaction proves too invasive, the fallback is a separate `commit_day_counts_ingested` mirror table. Not chosen, recorded for completeness.

---

## Section B — Rename `contributor_commits_ingested` → `commits_ingested`

The dedup table is a generic per-commit ledger once the heatmap shares it; the `contributor_` prefix is misleading.

Migration `066` is unmerged and exists only on this branch (and the local `cloudzilla_test` DB) — no deployment has the table. Therefore the rename is done by **editing migration 066 in place**: the file is renamed to `066_create_commits_ingested.sql` and its `CREATE TABLE` / `CREATE INDEX` use `commits_ingested` / `idx_commits_ingested_repo_user_week`. The local `cloudzilla_test` DB is dropped and re-created so the edited migration applies cleanly. No throwaway rename migration enters history.

`AttemptIngest`'s SQL and the store test from the prior task are updated to the new name.

---

## Section C — Backfill CLI

### Command

A new `stats` cobra parent under `cmd/cloudzilla` (today the CLI has only `migrate`), with one subcommand:

```
cloudzilla stats backfill [--repo <owner>/<name> | --all] [--apply]
```

- `--repo` / `--all` choose scope (exactly one required).
- **Dry-run is the default**: compute and print drift, write nothing.
- `--apply` performs the rewrite.

### Behaviour

Per repo:

1. Walk **every ref** (all branches) from root via the existing `CodeService` / go-git, collecting each distinct commit SHA once. Walking all refs (not just `default_branch`) and deduping by SHA matches what live deduped ingestion converges to — the authoritative set of commits currently reachable in the repo.
2. Recompute per-`(user, week)` and per-`(user, day)` sums from that commit set (resolving authors by email, as live ingestion does).
3. Diff the recomputed sums against current `contributor_week_stats` and `commit_day_counts`; print the drift.
4. With `--apply`, in one transaction: delete that repo's rows in `commits_ingested`, `contributor_week_stats`, and `commit_day_counts`, then re-ingest every collected commit through the Section A path.

Backfill is destructive — it overwrites existing aggregates for drifted repos, changing numbers shown on the contributors page and heatmap. The dry-run default ensures an admin sees the change set before committing to it.

---

## Section D — Release tag-name path-traversal fix

`ErrInvalidTagName` guards `Create` and `Update` in [release_service.go](../../../internal/service/release_service.go) with the regex `^[A-Za-z0-9._/-]{1,255}$`. That charset permits `.` and `/`, so `../etc/passwd` matches and is accepted — a path-traversal hole, and the cause of the two failing `release_service_test.go` tests.

Add structural validation alongside the charset regex, rejecting git-ref-invalid forms: a `..` substring, a leading or trailing `/`, a `//` substring, and a leading `.`. The duplicated check in `Create` and `Update` is extracted into one `validateTagName(string) error` helper. The two existing tests already specify the contract and will pass unchanged.

---

## Section E — Pull-store stale test

`PullStore.ListLinkedToIssue` ([pull_store.go:367](../../../internal/store/pull_store.go#L367)) returns PRs **explicitly linked via the `pull_issue_links` table** (the set the issue sidebar's link/unlink dropdown writes — a deliberate design from migration 060).

`TestPullStore_ListLinkedToIssue_ReturnsMatchingPRs` and its comment assert the obsolete **text-mention** behaviour (a PR "linked" because its title/body contains `#3`) and never seed any `pull_issue_links` rows — so the query correctly returns zero and the test fails. This is a stale test, not a production bug.

Rewrite the test to exercise the current contract: seed an issue, create PRs, insert `pull_issue_links` rows for the ones that should be linked, and assert `ListLinkedToIssue` returns exactly those. Correct the stale comment. No production code changes.

---

## Section F — Test-infrastructure hygiene

- **Leaked fixtures:** handler tests create bare-repo fixtures (`internal/handler/testuser_*/…`) in the source tree and the directory is not ignored. Add `internal/handler/testuser_*/` to `.gitignore`.
- **Test isolation:** the contributor-stats tests suffix fixture names with `os.Getpid()`, which is stable across repeated runs in one process — `go test -count=N` would collide on the `users.username` unique constraint. Replace those suffixes with the existing `testutil.UniqueSuffix(t)` helper (already used by `seedPullDeps`).

---

## Affected files

- `internal/db/migrations/066_create_commits_ingested.sql` — renamed from `066_create_contributor_commits_ingested.sql`, table renamed
- `internal/store/` — `contributor_stats_store.go` (rename table; unexported `dbtx` interface + `attemptIngest`/`addDelta` helpers; new `IngestCommitTx`; remove public `AttemptIngest`); `commit_stats_store.go` (`addCount` helper); `pull_store_test.go` (Section E); `contributor_stats_store_test.go` (test `IngestCommitTx` + `UniqueSuffix`)
- `internal/service/` — `contributor_stats_service.go` (`IngestCommit` delegates to `IngestCommitTx`); `repo_service.go` (`OnPostReceive` drops the batch heatmap call); `contributor_stats_service_test.go` (assert both aggregates + `UniqueSuffix`); `release_service.go` (Section D); `code_service_insights.go` (new `WalkAllRefCommits`)
- `cmd/cloudzilla/` — new `stats.go`: `stats` parent + `backfill` subcommand wired into `main.go`, calling a testable backfill function
- `.gitignore` — ignore leaked handler test fixtures

## Testing

TDD throughout. Integration tests run against the dedicated `cloudzilla_test` database (`TEST_DATABASE_DSN`). Key coverage:

- Double-ingest of one SHA leaves `commits = 1` in **both** `contributor_week_stats` and `commit_day_counts`.
- The two writes are transactional — a forced failure mid-`IngestCommit` leaves neither the claim nor the aggregate increment.
- Backfill dry-run reports drift without writing; `--apply` makes the aggregates match the recomputed set.
- Tag validation rejects `..`, leading/trailing `/`, `//`, leading `.`.
- The rewritten pull-store test passes against the explicit-link contract.

## Out of scope

- **In-process scheduler / cron** — no scheduling infrastructure exists in the codebase; backfill is a manually-invoked CLI. Building a scheduler is a separate, larger decision.
- **Reconciliation job** — transactional writes (Section A) eliminate the divergence it would have corrected.
- **Separate `commit_day_counts_ingested` table** — the shared-claim design supersedes it.
