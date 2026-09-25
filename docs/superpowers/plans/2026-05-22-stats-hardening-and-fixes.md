# Stats Hardening + Related Fixes — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Make commit-stats ingestion transactional and deduped for both contributor week-stats and the heatmap day-counts, add a backfill CLI, and fix two pre-existing bugs plus test-infra hygiene — all on branch `fix/contributor-stats-sha-dedup`.

**Architecture:** A single per-commit `IngestCommitTx` store method claims `(repo_id, sha)` in the renamed generic `commits_ingested` table and increments both `contributor_week_stats` and `commit_day_counts` inside one transaction. A `cloudzilla stats backfill` CLI recomputes aggregates from a full all-refs git walk. Tag-name validation gains structural path-traversal checks; a stale pull-store test is rewritten.

**Tech Stack:** Go 1.26+, PostgreSQL, `database/sql` (pgx driver), cobra, go-git v5, vanilla Go testing.

**Spec:** [`docs/superpowers/specs/2026-05-22-stats-hardening-and-fixes-design.md`](../specs/2026-05-22-stats-hardening-and-fixes-design.md)

**Test database:** integration tests and `make migrate` use the dedicated `cloudzilla_test` DB. DSN:
`postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable`
Set it as `TEST_DATABASE_DSN` for `go test`, and as `CZ_DATABASE_DSN` for `make migrate`.

**Pre-existing failures (NOT regressions — do not attribute to this work):** before Task 7/8 land, `go test ./...` shows `TestPullStore_ListLinkedToIssue_ReturnsMatchingPRs`, `TestReleaseService_Create_RejectsInvalidTagName`, `TestReleaseService_Update_RejectsInvalidTagName` failing. Task 7 fixes the release pair; Task 8 fixes the pull one.

## As shipped (2026-09-25)

All tasks are done. Where the branch differs from the steps below:

- **Migration number:** `073_create_commits_ingested.sql`, not 066. `main` had taken 066–069 and the phase-9 branch 070–072 by the time this landed. Test DB was fixed by dropping the old table and its `schema_migrations` row instead of recreating the DB.
- **Task 2/5:** the claim + week-stat + day-count sequence is one shared `ingestCommit(ctx, dbtx, repoID, CommitIngestRow)` helper used by both `IngestCommitTx` and `RebuildRepoStats`.
- **Task 3:** `OnPostReceive`'s early-return guard now checks `contributorStats` instead of the removed `commitStats`; all `NewRepoService` call sites (including tests) dropped the argument.
- **Task 6:** `stats backfill` skips repos with no git directory on disk instead of failing, continues past per-repo errors, and sets `SilenceUsage`.
- **Task 7:** `main` already rejected `..`; this branch extended the existing `validTagName` with the `//`, leading/trailing `/`, and leading `.` checks and added test cases for them.
- **Task 8:** already done on `main` (the test was rewritten there for explicit links); skipped.
- **Task 9:** `.gitignore` entry was already on `main`; the PID suffixes in the stats tests were replaced during Tasks 2–3. The remaining PID-suffixed tests are fixed on `tech/test-unique-suffix`.
- **Task 10:** `go test ./...` passes; `-count=2` needs `tech/test-unique-suffix`. New code is `errcheck`-clean.

---

## File structure

```
internal/
├── db/migrations/
│   └── 066_create_commits_ingested.sql          ← RENAMED from 066_create_contributor_commits_ingested.sql
├── store/
│   ├── contributor_stats_store.go               ← dbtx iface, helpers, IngestCommitTx, RebuildRepoStats
│   ├── contributor_stats_store_test.go          ← IngestCommitTx test
│   └── commit_stats_store.go                    ← addCount helper
├── service/
│   ├── contributor_stats_service.go             ← IngestCommit delegates to IngestCommitTx
│   ├── contributor_stats_service_test.go        ← assert both aggregates
│   ├── repo_service.go                          ← OnPostReceive drops batch heatmap call
│   ├── code_service_backfill.go                 ← NEW: WalkAllRefCommits
│   ├── code_service_backfill_test.go            ← NEW
│   ├── stats_backfill.go                        ← NEW: BackfillRepoStats
│   ├── stats_backfill_test.go                   ← NEW
│   └── release_service.go                       ← validateTagName helper
└── store/pull_store_test.go                     ← rewritten stale test
cmd/cloudzilla/
├── main.go                                      ← register stats command
└── stats.go                                     ← NEW: stats / backfill cobra commands
.gitignore                                       ← ignore leaked handler fixtures
```

---

## Task 1: Rename dedup table to `commits_ingested`

The dedup table becomes a generic per-commit ledger shared by contributor and heatmap stats; the `contributor_` prefix is dropped. Migration 066 is unmerged and exists only on this branch, so it is edited in place.

**Files:**
- Rename: `internal/db/migrations/066_create_contributor_commits_ingested.sql` → `066_create_commits_ingested.sql`
- Modify: `internal/store/contributor_stats_store.go`

- [x] **Step 1: Rename the migration file**

```bash
git mv internal/db/migrations/066_create_contributor_commits_ingested.sql internal/db/migrations/066_create_commits_ingested.sql
```

- [x] **Step 2: Replace the migration contents**

Overwrite `internal/db/migrations/066_create_commits_ingested.sql` with:

```sql
CREATE TABLE IF NOT EXISTS commits_ingested (
    repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    sha TEXT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    week DATE NOT NULL,
    additions INT NOT NULL DEFAULT 0,
    deletions INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (repo_id, sha)
);

CREATE INDEX IF NOT EXISTS idx_commits_ingested_repo_user_week
    ON commits_ingested (repo_id, user_id, week);
```

- [x] **Step 3: Update the table name in `AttemptIngest`**

In `internal/store/contributor_stats_store.go`, in the `AttemptIngest` method, change the SQL's first line from `INSERT INTO contributor_commits_ingested` to `INSERT INTO commits_ingested`. (This method is removed in Task 3; the rename here keeps the build green meanwhile.)

- [x] **Step 4: Recreate the test database**

The renamed migration cannot apply over a DB that already has the old-named table, so rebuild `cloudzilla_test`:

```bash
psql "postgres://cloudzilla:cloudzilla@localhost:5432/postgres?sslmode=disable" -c "DROP DATABASE IF EXISTS cloudzilla_test;"
psql "postgres://cloudzilla:cloudzilla@localhost:5432/postgres?sslmode=disable" -c "CREATE DATABASE cloudzilla_test OWNER cloudzilla;"
CZ_DATABASE_DSN="postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable" make migrate
```
Expected: migrations 001–066 apply, ending with `066_create_commits_ingested.sql`.

- [x] **Step 5: Verify the table**

```bash
psql "postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable" -c "\d commits_ingested"
```
Expected: table `commits_ingested` with `(repo_id, sha)` primary key and index `idx_commits_ingested_repo_user_week`.

- [x] **Step 6: Run store tests**

Run: `TEST_DATABASE_DSN="postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable" go test ./internal/store -run TestContributorStats`
Expected: PASS (`AttemptIngest` test still green — it calls the method, not the literal table name).

- [x] **Step 7: Commit**

```bash
git add internal/db/migrations/ internal/store/contributor_stats_store.go
git commit -m "refactor(db): rename contributor_commits_ingested to commits_ingested"
```

---

## Task 2: Transactional store layer — `dbtx`, write helpers, `IngestCommitTx`

Add a single transactional ingest method. The core of each write is extracted into an unexported helper taking a `dbtx` interface, so the same SQL serves both autocommit callers and the transaction. Public `AttemptIngest`/`AddDelta`/`AddCount` stay as wrappers (removed/kept as noted later) so the build stays green.

**Files:**
- Modify: `internal/store/contributor_stats_store.go`
- Modify: `internal/store/commit_stats_store.go`
- Modify: `internal/store/contributor_stats_store_test.go`

- [x] **Step 1: Write the failing test**

In `internal/store/contributor_stats_store_test.go`, add this test. It uses `testutil.UniqueSuffix` for per-run-unique isolation — add `"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"` to the file's import block if not present.

```go
func TestContributorStatsStore_IngestCommitTx_DedupesBothAggregates(t *testing.T) {
	db := openTestDBCommitStats(t)
	defer db.Close()

	s := store.NewContributorStatsStore(db)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)

	var userID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_superadmin) VALUES ($1, $2, 'x', false) RETURNING id`,
		"u_"+suffix, "u_"+suffix+"@test.invalid",
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var repoID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch) VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
		userID, "u_"+suffix, "repo_"+suffix,
	).Scan(&repoID); err != nil {
		t.Fatalf("insert repo: %v", err)
	}
	t.Cleanup(func() {
		db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})

	when := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	sha := "deadbeefcafe0001"

	if err := s.IngestCommitTx(ctx, repoID, userID, sha, when, 40, 8); err != nil {
		t.Fatalf("first IngestCommitTx: %v", err)
	}
	if err := s.IngestCommitTx(ctx, repoID, userID, sha, when, 40, 8); err != nil {
		t.Fatalf("second IngestCommitTx: %v", err)
	}

	weekRows, err := s.ListForRepo(ctx, repoID)
	if err != nil {
		t.Fatalf("ListForRepo: %v", err)
	}
	if len(weekRows) != 1 || weekRows[0].Commits != 1 || weekRows[0].Additions != 40 || weekRows[0].Deletions != 8 {
		t.Fatalf("week stats: want 1 row 1/40/8, got %+v", weekRows)
	}

	var dayRows, dayCount int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(commit_count), 0) FROM commit_day_counts WHERE repo_id = $1`, repoID,
	).Scan(&dayRows, &dayCount); err != nil {
		t.Fatalf("query day counts: %v", err)
	}
	if dayRows != 1 || dayCount != 1 {
		t.Fatalf("day counts: want 1 row summing to 1, got rows=%d sum=%d", dayRows, dayCount)
	}
}
```

- [x] **Step 2: Run the test to verify it fails**

Run: `TEST_DATABASE_DSN="postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable" go test ./internal/store -run TestContributorStatsStore_IngestCommitTx_DedupesBothAggregates -v`
Expected: FAIL — `s.IngestCommitTx undefined` (compile error).

- [x] **Step 3: Add the `dbtx` interface and write helpers in `contributor_stats_store.go`**

In `internal/store/contributor_stats_store.go`, add after the `import` block (the file already imports `context`, `database/sql`, `errors`, `time`):

```go
// dbtx is the subset of *sql.DB and *sql.Tx the stats write helpers need,
// so one query body serves both autocommit and transactional callers.
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// attemptIngest claims (repo_id, sha) in commits_ingested. Returns true iff the
// row was newly inserted (caller is the first writer). week must already be
// normalised to Monday UTC.
func attemptIngest(ctx context.Context, db dbtx, repoID, userID int64, sha string, week time.Time, additions, deletions int) (bool, error) {
	const q = `
		INSERT INTO commits_ingested (repo_id, sha, user_id, week, additions, deletions)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (repo_id, sha) DO NOTHING
		RETURNING 1`
	var one int
	err := db.QueryRowContext(ctx, q, repoID, sha, userID, week, additions, deletions).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// addDelta additively applies one commit's contribution to the per-week
// aggregate. week must already be normalised to Monday UTC.
func addDelta(ctx context.Context, db dbtx, repoID, userID int64, week time.Time, commits, additions, deletions int) error {
	const q = `
		INSERT INTO contributor_week_stats (repo_id, user_id, week, commits, additions, deletions, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (repo_id, user_id, week)
		DO UPDATE SET
			commits=contributor_week_stats.commits + EXCLUDED.commits,
			additions=contributor_week_stats.additions + EXCLUDED.additions,
			deletions=contributor_week_stats.deletions + EXCLUDED.deletions,
			updated_at=NOW()`
	_, err := db.ExecContext(ctx, q, repoID, userID, week, commits, additions, deletions)
	return err
}
```

- [x] **Step 4: Make `AddDelta` a wrapper over `addDelta`**

In `internal/store/contributor_stats_store.go`, replace the body of the existing `AddDelta` method with a wrapper (keep the exact public signature):

```go
func (s *ContributorStatsStore) AddDelta(ctx context.Context, repoID, userID int64, week time.Time, commits, additions, deletions int) error {
	return addDelta(ctx, s.db, repoID, userID, MondayUTC(week), commits, additions, deletions)
}
```

- [x] **Step 5: Make `AttemptIngest` a wrapper over `attemptIngest`**

In `internal/store/contributor_stats_store.go`, replace the body of `AttemptIngest` with a wrapper (keep its exact existing public signature — it is removed in Task 3):

```go
func (s *ContributorStatsStore) AttemptIngest(ctx context.Context, repoID int64, sha string, userID int64, week time.Time, additions, deletions int) (bool, error) {
	return attemptIngest(ctx, s.db, repoID, userID, sha, MondayUTC(week), additions, deletions)
}
```

- [x] **Step 6: Add the `addCount` helper and make `AddCount` a wrapper in `commit_stats_store.go`**

In `internal/store/commit_stats_store.go`, add the helper and rewrite `AddCount` as a wrapper. The `dbtx` interface is package-level (defined in `contributor_stats_store.go`, same `store` package):

```go
// addCount additively applies one commit's contribution to the per-day heatmap.
func addCount(ctx context.Context, db dbtx, repoID, userID int64, day time.Time, delta int) error {
	const q = `
		INSERT INTO commit_day_counts (repo_id, user_id, day, commit_count, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (repo_id, user_id, day)
		DO UPDATE SET commit_count = commit_day_counts.commit_count + EXCLUDED.commit_count, updated_at = NOW()`
	_, err := db.ExecContext(ctx, q, repoID, userID, day.UTC().Truncate(24*time.Hour), delta)
	return err
}

func (s *CommitStatsStore) AddCount(ctx context.Context, repoID, userID int64, day time.Time, delta int) error {
	return addCount(ctx, s.db, repoID, userID, day, delta)
}
```

- [x] **Step 7: Add `IngestCommitTx` to `contributor_stats_store.go`**

```go
// IngestCommitTx records one commit's contribution to both the per-week
// contributor aggregate and the per-day heatmap inside a single transaction.
// The commits_ingested claim dedupes by (repo_id, sha): an already-ingested SHA
// leaves both aggregates untouched.
func (s *ContributorStatsStore) IngestCommitTx(ctx context.Context, repoID, userID int64, sha string, when time.Time, additions, deletions int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	week := MondayUTC(when)
	claimed, err := attemptIngest(ctx, tx, repoID, userID, sha, week, additions, deletions)
	if err != nil {
		return err
	}
	if claimed {
		if err := addDelta(ctx, tx, repoID, userID, week, 1, additions, deletions); err != nil {
			return err
		}
		if err := addCount(ctx, tx, repoID, userID, when, 1); err != nil {
			return err
		}
	}
	return tx.Commit()
}
```

- [x] **Step 8: Run the new test to verify it passes**

Run: `TEST_DATABASE_DSN="postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable" go test ./internal/store -run TestContributorStatsStore_IngestCommitTx_DedupesBothAggregates -v`
Expected: PASS.

- [x] **Step 9: Run the full store package**

Run: `TEST_DATABASE_DSN="postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable" go test ./internal/store`
Expected: all PASS except the known pre-existing `TestPullStore_ListLinkedToIssue_ReturnsMatchingPRs`.

- [x] **Step 10: Commit**

```bash
git add internal/store/contributor_stats_store.go internal/store/commit_stats_store.go internal/store/contributor_stats_store_test.go
git commit -m "feat(store): IngestCommitTx — transactional claim + week-stat + day-count"
```

---

## Task 3: Switch over — `IngestCommit` delegates, `OnPostReceive` unifies

`ContributorStatsService.IngestCommit` becomes a delegate to `IngestCommitTx`; `OnPostReceive` drops the separate batch heatmap call; the now-unused public `AttemptIngest` is removed.

**Files:**
- Modify: `internal/service/contributor_stats_service.go`
- Modify: `internal/service/repo_service.go`
- Modify: `internal/service/services.go`
- Modify: `internal/service/contributor_stats_service_test.go`
- Modify: `internal/store/contributor_stats_store.go` (remove public `AttemptIngest`)
- Modify: `internal/store/contributor_stats_store_test.go` (remove its test)

- [x] **Step 1: Update the service test to assert both aggregates**

In `internal/service/contributor_stats_service_test.go`, replace the body of `TestContributorStatsService_IngestCommit_IsIdempotentBySha` (keep imports; add a raw day-count assertion). The full replacement test:

```go
func TestContributorStatsService_IngestCommit_IsIdempotentBySha(t *testing.T) {
	db := openTestDBContributorStatsService(t)
	defer db.Close()

	statsStore := store.NewContributorStatsStore(db)
	userStore := store.NewUserStore(db)
	svc := service.NewContributorStatsService(statsStore, userStore)

	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)

	var userID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_superadmin) VALUES ($1, $2, 'x', false) RETURNING id`,
		"u_"+suffix, "u_"+suffix+"@test.invalid",
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})
	var repoID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch) VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
		userID, "u_"+suffix, "repo_"+suffix,
	).Scan(&repoID); err != nil {
		t.Fatalf("insert repo: %v", err)
	}

	when := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	sha := "deadbeefcafe1234"

	if err := svc.IngestCommit(ctx, repoID, userID, when, sha, 50, 10); err != nil {
		t.Fatalf("first IngestCommit: %v", err)
	}
	if err := svc.IngestCommit(ctx, repoID, userID, when, sha, 50, 10); err != nil {
		t.Fatalf("second IngestCommit: %v", err)
	}

	rows, err := statsStore.ListForRepo(ctx, repoID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Commits != 1 || rows[0].Additions != 50 || rows[0].Deletions != 10 {
		t.Fatalf("week stats: want 1 row 1/50/10, got %+v", rows)
	}

	var dayCount int
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(commit_count), 0) FROM commit_day_counts WHERE repo_id = $1`, repoID,
	).Scan(&dayCount); err != nil {
		t.Fatalf("query day counts: %v", err)
	}
	if dayCount != 1 {
		t.Errorf("day counts: want sum 1, got %d", dayCount)
	}
}
```

Add `"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"` to this file's imports; remove the now-unused `"fmt"` and `"os"` imports if they become unused (they were only used to build the old PID suffix — verify with `go build`).

- [x] **Step 2: Run the test to verify it fails**

Run: `TEST_DATABASE_DSN="postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable" go test ./internal/service -run TestContributorStatsService_IngestCommit_IsIdempotentBySha -v`
Expected: FAIL on `day counts: want sum 1, got 0` — the current `IngestCommit` only writes the week aggregate.

- [x] **Step 3: Rewrite `IngestCommit` to delegate**

In `internal/service/contributor_stats_service.go`, replace the `IngestCommit` body (keep the exact signature):

```go
func (s *ContributorStatsService) IngestCommit(ctx context.Context, repoID, userID int64, when time.Time, sha string, additions, deletions int) error {
	return s.stats.IngestCommitTx(ctx, repoID, userID, sha, when, additions, deletions)
}
```

Remove any imports in this file that become unused (e.g. `store` may still be needed elsewhere — verify with `go build`).

- [x] **Step 4: Run the test to verify it passes**

Run: `TEST_DATABASE_DSN="postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable" go test ./internal/service -run TestContributorStatsService_IngestCommit_IsIdempotentBySha -v`
Expected: PASS.

- [x] **Step 5: Drop the batch heatmap call from `OnPostReceive`**

In `internal/service/repo_service.go`, inside `OnPostReceive`, delete this block (the `samples` build and the `commitStats.Ingest` call):

```go
	samples := make([]CommitSample, len(commits))
	for i, c := range commits {
		samples[i] = CommitSample{AuthorEmail: c.AuthorEmail, Time: c.AuthorTime}
	}
	if err := s.commitStats.Ingest(ctx, repo.ID, samples); err != nil {
		return fmt.Errorf("commit stats ingest (repo_id=%d, samples=%d): %w", repo.ID, len(samples), err)
	}
```

The per-commit contributor loop that follows is unchanged — it now drives both aggregates via the delegated `IngestCommit`.

- [x] **Step 6: Remove the now-unused `commitStats` dependency from `RepoService`**

Run: `grep -n "commitStats" internal/service/repo_service.go`
If the only remaining references are the struct field and the constructor parameter (no method calls), remove them:
- Delete the `commitStats *CommitStatsService` field from the `RepoService` struct.
- Remove the `commitStats` parameter from `NewRepoService` and its assignment in the constructor body.

Then in `internal/service/services.go`, update the `NewRepoService(...)` call to drop the `commitStatsSvc` argument. The current call is:
```go
repoSvc := NewRepoService(stores.Repo, stores.User, stores.Org, commitStatsSvc, contributorStatsSvc, code, cfg.Git)
```
becomes:
```go
repoSvc := NewRepoService(stores.Repo, stores.User, stores.Org, contributorStatsSvc, code, cfg.Git)
```
`commitStatsSvc` itself stays — it is still assigned to `Services.CommitStats` and used by `BackfillRecentCommits`.

If `grep` shows `commitStats` is still used elsewhere in `repo_service.go`, keep the field and skip this step.

- [x] **Step 7: Remove the obsolete public `AttemptIngest` and its test**

In `internal/store/contributor_stats_store.go`, delete the public `AttemptIngest` method (the wrapper added in Task 2 — its logic lives in the `attemptIngest` helper and `IngestCommitTx`).

In `internal/store/contributor_stats_store_test.go`, delete the test `TestContributorStatsStore_AttemptIngest_DedupesBySha` (superseded by `TestContributorStatsStore_IngestCommitTx_DedupesBothAggregates`).

- [x] **Step 8: Build everything**

Run: `go build ./...`
Expected: clean. If any `NewRepoService` call site outside `services.go` fails to compile, drop its commit-stats-service argument the same way as Step 6.

- [x] **Step 9: Run service + store + handler tests**

Run: `TEST_DATABASE_DSN="postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable" go test ./internal/service ./internal/store ./internal/handler`
Expected: all PASS except the three known pre-existing failures (`TestPullStore_ListLinkedToIssue_ReturnsMatchingPRs`, `TestReleaseService_Create_RejectsInvalidTagName`, `TestReleaseService_Update_RejectsInvalidTagName`).

- [x] **Step 10: Commit**

```bash
git add internal/service/ internal/store/contributor_stats_store.go internal/store/contributor_stats_store_test.go
git commit -m "feat(stats): unify post-receive ingest behind transactional IngestCommitTx"
```

---

## Task 4: `CodeService.WalkAllRefCommits`

A full all-refs commit walk for the backfill — visits every commit reachable from any branch exactly once, with per-commit line stats.

**Files:**
- Create: `internal/service/code_service_backfill.go`
- Create: `internal/service/code_service_backfill_test.go`

- [x] **Step 1: Write the failing test**

Create `internal/service/code_service_backfill_test.go`:

```go
package service_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

// buildMultiBranchRepo creates a git repo at <reposRoot>/<owner>/<repo>.git with
// commits on `main` and a `feature` branch, and returns reposRoot. The repo is
// non-bare; CodeService only reads it, so non-bare is fine.
func buildMultiBranchRepo(t *testing.T, owner, repoName string) string {
	t.Helper()
	reposRoot := t.TempDir()
	dir := filepath.Join(reposRoot, owner, repoName+".git")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	commit := func(file, content string) {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if _, err := wt.Add(file); err != nil {
			t.Fatalf("add: %v", err)
		}
		if _, err := wt.Commit("c "+file, &gogit.CommitOptions{
			Author: &object.Signature{Name: "T", Email: "t@test.invalid", When: time.Now()},
		}); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}
	commit("a.txt", "a\n")
	commit("b.txt", "b\n")
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if err := repo.Storer.SetReference(plumbing.NewHashReference("refs/heads/feature", head.Hash())); err != nil {
		t.Fatalf("set feature ref: %v", err)
	}
	if err := wt.Checkout(&gogit.CheckoutOptions{Branch: "refs/heads/feature"}); err != nil {
		t.Fatalf("checkout feature: %v", err)
	}
	commit("c.txt", "c\n")
	return reposRoot
}

func TestCodeService_WalkAllRefCommits_VisitsEveryRefDeduped(t *testing.T) {
	reposRoot := buildMultiBranchRepo(t, "alice", "proj")
	code := service.NewCodeService(config.GitConfig{ReposRoot: reposRoot})

	commits, err := code.WalkAllRefCommits("alice", "proj")
	if err != nil {
		t.Fatalf("WalkAllRefCommits: %v", err)
	}
	// 3 distinct commits: a.txt, b.txt on main; c.txt on feature.
	if len(commits) != 3 {
		t.Fatalf("want 3 unique commits, got %d: %+v", len(commits), commits)
	}
	seen := map[string]bool{}
	for _, c := range commits {
		if seen[c.SHA] {
			t.Errorf("duplicate SHA %s", c.SHA)
		}
		seen[c.SHA] = true
		if c.AuthorEmail != "t@test.invalid" {
			t.Errorf("author email: got %q", c.AuthorEmail)
		}
		if c.Additions < 1 {
			t.Errorf("commit %s: want additions >= 1, got %d", c.SHA, c.Additions)
		}
	}
}
```

- [x] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/service -run TestCodeService_WalkAllRefCommits_VisitsEveryRefDeduped -v`
Expected: FAIL — `code.WalkAllRefCommits undefined` (compile error).

- [x] **Step 3: Implement `WalkAllRefCommits`**

Create `internal/service/code_service_backfill.go`:

```go
package service

import (
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// WalkedCommit is one commit visited by WalkAllRefCommits.
type WalkedCommit struct {
	SHA         string
	AuthorEmail string
	When        time.Time
	Additions   int
	Deletions   int
}

// WalkAllRefCommits visits every commit reachable from any branch of the repo,
// each exactly once, with per-commit line stats. It is the authoritative commit
// set used by the stats backfill.
func (s *CodeService) WalkAllRefCommits(owner, repoName string) ([]WalkedCommit, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, err
	}
	branches, err := repo.Branches()
	if err != nil {
		return nil, err
	}
	seen := make(map[plumbing.Hash]struct{})
	var out []WalkedCommit
	err = branches.ForEach(func(ref *plumbing.Reference) error {
		iter, err := repo.Log(&gogit.LogOptions{From: ref.Hash()})
		if err != nil {
			return err
		}
		defer iter.Close()
		return iter.ForEach(func(c *object.Commit) error {
			if _, dup := seen[c.Hash]; dup {
				return nil
			}
			seen[c.Hash] = struct{}{}
			stats, err := c.Stats()
			if err != nil {
				return err
			}
			add, del := 0, 0
			for _, fs := range stats {
				add += fs.Addition
				del += fs.Deletion
			}
			out = append(out, WalkedCommit{
				SHA:         c.Hash.String(),
				AuthorEmail: c.Author.Email,
				When:        c.Author.When,
				Additions:   add,
				Deletions:   del,
			})
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
```

- [x] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/service -run TestCodeService_WalkAllRefCommits_VisitsEveryRefDeduped -v`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add internal/service/code_service_backfill.go internal/service/code_service_backfill_test.go
git commit -m "feat(code): WalkAllRefCommits — full all-refs commit walk for backfill"
```

---

## Task 5: Backfill core — `RebuildRepoStats` + `BackfillRepoStats`

The store method that atomically rewrites a repo's three stats tables, and the service function that computes drift (dry-run) or applies it.

**Files:**
- Modify: `internal/store/contributor_stats_store.go` (add `CommitIngestRow`, `RebuildRepoStats`)
- Create: `internal/service/stats_backfill.go`
- Create: `internal/service/stats_backfill_test.go`

- [x] **Step 1: Add `RebuildRepoStats` to the store**

In `internal/store/contributor_stats_store.go`, add:

```go
// CommitIngestRow is one commit's resolved contribution, fed to RebuildRepoStats.
type CommitIngestRow struct {
	UserID    int64
	SHA       string
	When      time.Time
	Additions int
	Deletions int
}

// RebuildRepoStats atomically replaces all of a repo's stats: it clears the
// repo's rows in commits_ingested, contributor_week_stats, and commit_day_counts,
// then re-ingests the given commits — all in one transaction.
func (s *ContributorStatsStore) RebuildRepoStats(ctx context.Context, repoID int64, rows []CommitIngestRow) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, table := range []string{"commits_ingested", "contributor_week_stats", "commit_day_counts"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE repo_id = $1`, repoID); err != nil {
			return err
		}
	}
	for _, r := range rows {
		week := MondayUTC(r.When)
		claimed, err := attemptIngest(ctx, tx, repoID, r.UserID, r.SHA, week, r.Additions, r.Deletions)
		if err != nil {
			return err
		}
		if claimed {
			if err := addDelta(ctx, tx, repoID, r.UserID, week, 1, r.Additions, r.Deletions); err != nil {
				return err
			}
			if err := addCount(ctx, tx, repoID, r.UserID, r.When, 1); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
```

The table names are a fixed in-code whitelist, never user input — the string concatenation is injection-safe.

- [x] **Step 2: Write the failing test for `BackfillRepoStats`**

Create `internal/service/stats_backfill_test.go`. It reuses `buildMultiBranchRepo` from Task 4's test file (same `service_test` package):

```go
package service_test

import (
	"context"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestBackfillRepoStats_DryRunThenApply(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)

	// The git author email below is "t@test.invalid"; the user must match it.
	var userID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_superadmin) VALUES ($1, 't@test.invalid', 'x', false) RETURNING id`,
		"u_"+suffix,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() { db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, userID) })
	var repoID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch) VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
		userID, "alice_"+suffix, "proj_"+suffix,
	).Scan(&repoID); err != nil {
		t.Fatalf("insert repo: %v", err)
	}

	reposRoot := buildMultiBranchRepo(t, "alice_"+suffix, "proj_"+suffix)
	code := service.NewCodeService(config.GitConfig{ReposRoot: reposRoot})
	statsStore := store.NewContributorStatsStore(db)
	userStore := store.NewUserStore(db)
	repo := model.Repository{ID: repoID, OwnerName: "alice_" + suffix, Name: "proj_" + suffix, DefaultBranch: "main"}

	// Dry run — reports 3 commits, writes nothing.
	dry, err := service.BackfillRepoStats(ctx, statsStore, code, userStore, repo, false)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if dry.CommitsWithKnownUser != 3 || dry.WeekCommitsBefore != 0 || dry.Applied {
		t.Fatalf("dry-run report wrong: %+v", dry)
	}
	weekRows, _ := statsStore.ListForRepo(ctx, repoID)
	if len(weekRows) != 0 {
		t.Fatalf("dry-run must not write: got %d week rows", len(weekRows))
	}

	// Apply — writes the recomputed aggregates.
	applied, err := service.BackfillRepoStats(ctx, statsStore, code, userStore, repo, true)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !applied.Applied || applied.WeekCommitsAfter != 3 {
		t.Fatalf("apply report wrong: %+v", applied)
	}
	weekRows, _ = statsStore.ListForRepo(ctx, repoID)
	total := 0
	for _, r := range weekRows {
		total += r.Commits
	}
	if total != 3 {
		t.Fatalf("after apply: want 3 week commits, got %d", total)
	}
}
```

- [x] **Step 3: Run the test to verify it fails**

Run: `TEST_DATABASE_DSN="postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable" go test ./internal/service -run TestBackfillRepoStats_DryRunThenApply -v`
Expected: FAIL — `service.BackfillRepoStats undefined` (compile error).

- [x] **Step 4: Implement `BackfillRepoStats`**

Create `internal/service/stats_backfill.go`:

```go
package service

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// BackfillReport is the per-repo outcome of a stats backfill.
type BackfillReport struct {
	Repo                 string
	CommitsWalked        int
	CommitsWithKnownUser int
	WeekCommitsBefore    int
	WeekCommitsAfter     int
	Applied              bool
}

// BackfillRepoStats recomputes a repo's contributor and heatmap aggregates from
// a full all-refs git walk. With apply=false it only reports drift; with
// apply=true it atomically rewrites the repo's stats tables.
func BackfillRepoStats(ctx context.Context, stats *store.ContributorStatsStore, code *CodeService, users *store.UserStore, repo model.Repository, apply bool) (BackfillReport, error) {
	walked, err := code.WalkAllRefCommits(repo.OwnerName, repo.Name)
	if err != nil {
		return BackfillReport{}, err
	}

	emailToUser := make(map[string]int64)
	var rows []store.CommitIngestRow
	for _, c := range walked {
		uid, resolved := emailToUser[c.AuthorEmail]
		if !resolved {
			u, err := users.GetByEmail(ctx, c.AuthorEmail)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return BackfillReport{}, err
			}
			if u != nil {
				uid = u.ID
			}
			emailToUser[c.AuthorEmail] = uid
		}
		if uid == 0 {
			continue
		}
		rows = append(rows, store.CommitIngestRow{
			UserID: uid, SHA: c.SHA, When: c.When, Additions: c.Additions, Deletions: c.Deletions,
		})
	}

	before, err := stats.ListForRepo(ctx, repo.ID)
	if err != nil {
		return BackfillReport{}, err
	}
	weekCommitsBefore := 0
	for _, r := range before {
		weekCommitsBefore += r.Commits
	}

	report := BackfillReport{
		Repo:                 repo.OwnerName + "/" + repo.Name,
		CommitsWalked:        len(walked),
		CommitsWithKnownUser: len(rows),
		WeekCommitsBefore:    weekCommitsBefore,
		WeekCommitsAfter:     len(rows),
		Applied:              apply,
	}
	if apply {
		if err := stats.RebuildRepoStats(ctx, repo.ID, rows); err != nil {
			return BackfillReport{}, err
		}
	}
	return report, nil
}
```

`WeekCommitsAfter` equals `len(rows)` because each distinct commit with a known author contributes exactly one to the deduped week total.

- [x] **Step 5: Run the test to verify it passes**

Run: `TEST_DATABASE_DSN="postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable" go test ./internal/service -run TestBackfillRepoStats_DryRunThenApply -v`
Expected: PASS.

- [x] **Step 6: Commit**

```bash
git add internal/store/contributor_stats_store.go internal/service/stats_backfill.go internal/service/stats_backfill_test.go
git commit -m "feat(stats): backfill core — RebuildRepoStats and BackfillRepoStats"
```

---

## Task 6: `cloudzilla stats backfill` CLI command

A thin cobra wrapper over `BackfillRepoStats`: dry-run by default, `--apply` to write, `--repo` / `--all` scope.

**Files:**
- Create: `cmd/cloudzilla/stats.go`
- Modify: `cmd/cloudzilla/main.go`

- [x] **Step 1: Create the stats command**

Create `cmd/cloudzilla/stats.go`:

```go
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/db"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

func statsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "stats", Short: "Statistics maintenance commands"}
	cmd.AddCommand(statsBackfillCmd())
	return cmd
}

func statsBackfillCmd() *cobra.Command {
	var repoArg string
	var allRepos bool
	var apply bool

	c := &cobra.Command{
		Use:   "backfill",
		Short: "Recompute contributor and heatmap stats from git history",
		Long:  "Dry-run by default: reports drift without writing. Pass --apply to rewrite the aggregates.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if (repoArg != "") == allRepos {
				return fmt.Errorf("exactly one of --repo or --all is required")
			}
			cfg, err := config.Load(cfgFile)
			if err != nil {
				return err
			}
			database, err := db.Connect(cfg.Database)
			if err != nil {
				return err
			}
			defer database.Close()

			stores := store.New(database)
			code := service.NewCodeService(cfg.Git)
			ctx := context.Background()

			var repos []model.Repository
			if allRepos {
				repos, err = stores.Repo.ListAll(ctx)
				if err != nil {
					return err
				}
			} else {
				owner, name, ok := strings.Cut(repoArg, "/")
				if !ok || owner == "" || name == "" {
					return fmt.Errorf("--repo must be owner/name, got %q", repoArg)
				}
				r, err := stores.Repo.GetByOwnerName(ctx, owner, name)
				if err != nil {
					return err
				}
				repos = []model.Repository{*r}
			}

			for _, r := range repos {
				report, err := service.BackfillRepoStats(ctx, stores.ContributorStats, code, stores.User, r, apply)
				if err != nil {
					return fmt.Errorf("backfill %s/%s: %w", r.OwnerName, r.Name, err)
				}
				verb := "would set"
				if apply {
					verb = "set"
				}
				fmt.Printf("%-40s walked=%d known-user=%d week-commits %d -> %s %d\n",
					report.Repo, report.CommitsWalked, report.CommitsWithKnownUser,
					report.WeekCommitsBefore, verb, report.WeekCommitsAfter)
			}
			if !apply {
				fmt.Println("\n(dry-run — re-run with --apply to write changes)")
			}
			return nil
		},
	}
	c.Flags().StringVar(&repoArg, "repo", "", "repository as owner/name")
	c.Flags().BoolVar(&allRepos, "all", false, "backfill every repository")
	c.Flags().BoolVar(&apply, "apply", false, "write changes (default: dry-run)")
	return c
}
```

- [x] **Step 2: Register the command in `main.go`**

In `cmd/cloudzilla/main.go`, in the `init()` function, add the stats command next to the existing migrate registration:

```go
func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: config.yaml)")
	rootCmd.AddCommand(migrateCmd())
	rootCmd.AddCommand(statsCmd())
}
```

- [x] **Step 3: Build**

Run: `go build ./...`
Expected: clean.

- [x] **Step 4: Smoke-test the command**

Run:
```bash
go run ./cmd/cloudzilla stats backfill --help
go run ./cmd/cloudzilla stats backfill
```
Expected: the first prints usage including `--repo`, `--all`, `--apply`; the second exits non-zero with `exactly one of --repo or --all is required`.

- [x] **Step 5: Commit**

```bash
git add cmd/cloudzilla/stats.go cmd/cloudzilla/main.go
git commit -m "feat(cli): cloudzilla stats backfill command"
```

---

## Task 7: Release tag-name path-traversal fix

The charset regex permits `.` and `/`, so `../etc/passwd` is accepted. Add structural git-ref-hygiene checks in a shared helper.

**Files:**
- Modify: `internal/service/release_service.go`

- [x] **Step 1: Confirm the failing tests fail**

Run: `TEST_DATABASE_DSN="postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable" go test ./internal/service -run "TestReleaseService_(Create|Update)_RejectsInvalidTagName" -v`
Expected: both FAIL on the `../etc/passwd` case (`want ErrInvalidTagName, got <nil>`).

- [x] **Step 2: Add the `validateTagName` helper**

In `internal/service/release_service.go`, add `"strings"` to the import block, and add this helper near the `tagNamePattern` var:

```go
// validateTagName rejects tag names that the charset pattern alone would allow
// through but which are unsafe as git refs or filesystem paths — notably ".."
// path-traversal sequences.
func validateTagName(tagName string) error {
	if !tagNamePattern.MatchString(tagName) {
		return ErrInvalidTagName
	}
	if strings.Contains(tagName, "..") ||
		strings.Contains(tagName, "//") ||
		strings.HasPrefix(tagName, "/") ||
		strings.HasSuffix(tagName, "/") ||
		strings.HasPrefix(tagName, ".") {
		return ErrInvalidTagName
	}
	return nil
}
```

- [x] **Step 3: Use the helper in `Create` and `Update`**

In `internal/service/release_service.go`, replace the tag guard at the top of `Create`:
```go
	if !tagNamePattern.MatchString(tagName) {
		return nil, ErrInvalidTagName
	}
```
with:
```go
	if err := validateTagName(tagName); err != nil {
		return nil, err
	}
```
Apply the identical replacement to the same guard at the top of `Update`.

- [x] **Step 4: Run the tests to verify they pass**

Run: `TEST_DATABASE_DSN="postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable" go test ./internal/service -run "TestReleaseService_(Create|Update)_RejectsInvalidTagName" -v`
Expected: both PASS.

- [x] **Step 5: Commit**

```bash
git add internal/service/release_service.go
git commit -m "fix(releases): reject path-traversal sequences in tag names"
```

---

## Task 8: Rewrite the stale pull-store linked-issue test

`ListLinkedToIssue` returns PRs linked via the explicit `pull_issue_links` table. The existing test asserts obsolete text-mention behavior and never seeds links. Rewrite it.

**Files:**
- Modify: `internal/store/pull_store_test.go`

- [x] **Step 1: Replace the stale test**

In `internal/store/pull_store_test.go`, delete the test `TestPullStore_ListLinkedToIssue_ReturnsMatchingPRs` and its two-line `//` comment above it, and add this in its place:

```go
// TestPullStore_ListLinkedToIssue_ReturnsExplicitlyLinkedPRs verifies that
// ListLinkedToIssue returns exactly the PRs joined to the issue via the
// pull_issue_links table, and excludes PRs with no such link.
func TestPullStore_ListLinkedToIssue_ReturnsExplicitlyLinkedPRs(t *testing.T) {
	ps, repoID, ownerID := seedPullDeps(t)
	db := testutil.OpenTestDB(t)
	ctx := context.Background()

	issue := &model.Issue{RepoID: repoID, AuthorID: ownerID, Title: "Crash on startup"}
	if err := store.NewIssueStore(db).Create(ctx, issue); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	linkedA := &model.PullRequest{
		RepoID: repoID, AuthorID: ownerID, Title: "Fix A", Body: "",
		HeadBranch: "a", BaseBranch: "main", State: model.PRStateOpen,
	}
	linkedB := &model.PullRequest{
		RepoID: repoID, AuthorID: ownerID, Title: "Fix B", Body: "",
		HeadBranch: "b", BaseBranch: "main", State: model.PRStateOpen,
	}
	unrelated := &model.PullRequest{
		RepoID: repoID, AuthorID: ownerID, Title: "Unrelated", Body: "",
		HeadBranch: "u", BaseBranch: "main", State: model.PRStateOpen,
	}
	for _, pr := range []*model.PullRequest{linkedA, linkedB, unrelated} {
		if err := ps.Create(ctx, pr); err != nil {
			t.Fatalf("create pr: %v", err)
		}
	}
	for _, pr := range []*model.PullRequest{linkedA, linkedB} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO pull_issue_links (pull_id, issue_id) VALUES ($1, $2)`, pr.ID, issue.ID,
		); err != nil {
			t.Fatalf("insert link: %v", err)
		}
	}

	linked, err := ps.ListLinkedToIssue(ctx, repoID, issue.Number)
	if err != nil {
		t.Fatalf("ListLinkedToIssue: %v", err)
	}
	if len(linked) != 2 {
		t.Fatalf("want 2 linked PRs, got %d", len(linked))
	}
	for _, pr := range linked {
		if pr.ID == unrelated.ID {
			t.Error("unrelated PR must not appear in linked results")
		}
	}
}
```

If `internal/store/pull_store_test.go` does not already import `"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"` and `"github.com/mkappworks-dev/cloudzilla-app/internal/model"`, add them (the file already uses `seedPullDeps`, which lives in the same file, and `model.PullRequest`, so `model` is likely already imported — verify with `go build`).

- [x] **Step 2: Run the test**

Run: `TEST_DATABASE_DSN="postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable" go test ./internal/store -run TestPullStore_ListLinkedToIssue_ReturnsExplicitlyLinkedPRs -v`
Expected: PASS.

- [x] **Step 3: Commit**

```bash
git add internal/store/pull_store_test.go
git commit -m "test(store): rewrite stale ListLinkedToIssue test for explicit-link contract"
```

---

## Task 9: Test-infra hygiene — `.gitignore` and per-run isolation

**Files:**
- Modify: `.gitignore`
- Modify: `internal/store/contributor_stats_store_test.go`

- [x] **Step 1: Ignore leaked handler test fixtures**

In `.gitignore`, under the `# UI design prototypes` block or near the other test-artifact entries, add:

```
# Leaked handler test fixtures
internal/handler/testuser_*/
```

- [x] **Step 2: Replace the remaining PID-only suffix in the store test**

In `internal/store/contributor_stats_store_test.go`, find the test `TestContributorStatsStore_UpsertAndList`. It builds its suffix with `os.Getpid()` (e.g. `fmt.Sprintf("cws_%d", os.Getpid())`). Replace that with `testutil.UniqueSuffix(t)` — for example, a line like:

```go
	suffix := fmt.Sprintf("cws_%d", os.Getpid())
```
becomes:
```go
	suffix := "cws_" + testutil.UniqueSuffix(t)
```

`testutil` is already imported by this file (added in Task 2). After the edit, run `go vet ./internal/store` and remove `"os"` and/or `"fmt"` from the import block if they are no longer used anywhere in the file.

- [x] **Step 3: Confirm no PID-only suffixes remain in the stats tests**

Run: `grep -rn "os.Getpid" internal/store/contributor_stats_store_test.go internal/service/contributor_stats_service_test.go`
Expected: no matches.

- [x] **Step 4: Run the affected packages, twice, to prove isolation**

Run: `TEST_DATABASE_DSN="postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable" go test ./internal/store ./internal/service -count=2`
Expected: PASS on both runs (no `users.username` unique-constraint collisions) except the three known pre-existing failures, which Tasks 7 and 8 have by now fixed — so this run should be fully green.

- [x] **Step 5: Commit**

```bash
git add .gitignore internal/store/contributor_stats_store_test.go
git commit -m "test: gitignore leaked fixtures, use UniqueSuffix for per-run isolation"
```

---

## Task 10: Full-suite verification

- [x] **Step 1: Run the entire suite**

Run: `TEST_DATABASE_DSN="postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable" go test ./...`
Expected: all packages PASS — including the previously-failing `TestPullStore_ListLinkedToIssue_*` and `TestReleaseService_*_RejectsInvalidTagName`, now fixed. No leaked `internal/handler/testuser_*` directories tracked by git (`git status --short` clean apart from intended changes).

- [x] **Step 2: Build the binary**

Run: `go build ./...`
Expected: clean.

If anything fails, fix it before considering the plan complete.

---

## Self-review

- **Spec coverage:** Section A → Tasks 2–3 (transactional unified ingest); Section B → Task 1 (rename); Section C → Tasks 4–6 (`WalkAllRefCommits`, backfill core, CLI); Section D → Task 7 (tag validation); Section E → Task 8 (stale test); Section F → Task 9 (`.gitignore` + isolation). Task 10 is whole-suite verification. The spec's out-of-scope items (scheduler/cron, reconciliation job, separate `commit_day_counts_ingested` table) are correctly absent.
- **Placeholders:** none — every code step shows the full code; every command shows expected output. The one conditional (Task 3 Step 6: remove `commitStats` only if grep shows it unused) is a deterministic verification with the exact edits given for the expected case.
- **Type consistency:** `IngestCommitTx(ctx, repoID, userID int64, sha string, when time.Time, additions, deletions int)` is identical in Task 2's definition and Task 3's call. `attemptIngest`/`addDelta`/`addCount` take `dbtx` and normalised inputs consistently in Tasks 2 and 5. `WalkedCommit` (Task 4) feeds `store.CommitIngestRow` (Task 5) with matching field names. `BackfillRepoStats(ctx, stats, code, users, repo, apply)` is identical in Task 5's definition and Task 6's call. `BackfillReport` fields used in Task 6's `Printf` (`Repo`, `CommitsWalked`, `CommitsWithKnownUser`, `WeekCommitsBefore`, `WeekCommitsAfter`) all exist in Task 5's struct.
- **Build-green between tasks:** Task 1 keeps `AttemptIngest` (renamed table only); Task 2 keeps public wrappers so `IngestCommit` still compiles; Task 3 switches `IngestCommit` then removes the dead wrapper in the same task. Every task ends on a compiling, committable state.
