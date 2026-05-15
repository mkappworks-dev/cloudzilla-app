# Contributor stats: cross-push SHA dedup — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `ContributorStatsService.IngestCommit` idempotent across push invocations so force-pushes, rebases, and overlapping-branch pushes can no longer double-count the same commit SHA in `contributor_week_stats`.

**Architecture:** Add a `contributor_commits_ingested(repo_id, sha PK)` side table populated atomically via `INSERT ... ON CONFLICT DO NOTHING RETURNING`. The store returns a boolean "claimed" flag; the service only calls `AddDelta` on the per-week aggregate when the claim succeeds. Two writes per new commit (claim row + bucket increment) versus one today; second writers short-circuit on conflict. Non-transactional by design — see spec on the trade-off.

**Tech Stack:** Go 1.23+, PostgreSQL 14+, sqlx/database/sql, vanilla Go testing.

**Spec:** [`docs/superpowers/specs/2026-05-15-contributor-stats-sha-dedup-design.md`](../specs/2026-05-15-contributor-stats-sha-dedup-design.md)

**Decisions locked from spec open questions:**
1. Keep the in-memory `seen` map in `OnPostReceive` as a cheap pre-filter — no harm, saves the `GetCommit` patch walk for in-push duplicates.
2. Foreign keys use `ON DELETE CASCADE` to match `contributor_week_stats`.
3. The two-write sequence is non-transactional; reconciliation is left as a future cron (out of scope).

---

## File structure

```
internal/
├── db/migrations/
│   └── 056_create_contributor_commits_ingested.sql   ← NEW
├── store/
│   ├── contributor_stats_store.go                    ← add AttemptIngest
│   └── contributor_stats_store_test.go               ← add AttemptIngest test
└── service/
    ├── contributor_stats_service.go                  ← IngestCommit gains sha
    └── repo_service.go                               ← pass c.SHA through
```

Responsibilities:
- Migration owns schema only.
- `AttemptIngest` is the dedup primitive — atomic claim, returns whether the caller is the first writer.
- `IngestCommit` composes the claim and the additive aggregate increment; remains the only public ingest entry point.
- `OnPostReceive` is unchanged in shape — it gains one new argument on the inner call.

---

## Task 1: Migration 056 — `contributor_commits_ingested` table

**Files:**
- Create: `internal/db/migrations/056_create_contributor_commits_ingested.sql`

- [ ] **Step 1: Write the migration**

Create `internal/db/migrations/056_create_contributor_commits_ingested.sql`:

```sql
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

- [ ] **Step 2: Verify migration order, then apply**

Pre-check that 055 is already applied (otherwise `make migrate` will run it first, which is fine — just makes the output noisier):

```bash
psql "$CZ_DATABASE_DSN" -c "\d contributor_week_stats"
```
Expected: table exists.

Then:

```bash
make migrate
```
Expected: `056_create_contributor_commits_ingested.sql ... ok`.

Confirm:

```bash
psql "$CZ_DATABASE_DSN" -c "\d contributor_commits_ingested"
```
Expected: shows the table with `(repo_id, sha)` primary key.

Note: `created_at` is intentionally not read or written by the code in this plan. It is an audit field for the future reconciliation cron; do not "tidy it away."

- [ ] **Step 3: Commit**

```bash
git add internal/db/migrations/056_create_contributor_commits_ingested.sql
git commit -m "feat(db): migration 056 — contributor_commits_ingested dedup table"
```

---

## Task 2: Store `AttemptIngest` — atomic claim primitive (TDD)

**Files:**
- Modify: `internal/store/contributor_stats_store.go`
- Modify: `internal/store/contributor_stats_store_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/store/contributor_stats_store_test.go`:

```go
func TestContributorStatsStore_AttemptIngest_DedupesBySha(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set; skipping integration test")
	}
	db := openTestDBCommitStats(t)
	defer db.Close()

	s := store.NewContributorStatsStore(db)
	ctx := context.Background()
	suffix := fmt.Sprintf("cci_%d", os.Getpid())

	var userID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_superadmin) VALUES ($1, $2, 'x', false) RETURNING id`,
		suffix, suffix+"@test.invalid",
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var repoID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch) VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
		userID, suffix, "repo_"+suffix,
	).Scan(&repoID); err != nil {
		t.Fatalf("insert repo: %v", err)
	}
	t.Cleanup(func() {
		db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})

	week := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	sha := "abc123def4567890"

	inserted, err := s.AttemptIngest(ctx, repoID, sha, userID, week, 10, 5)
	if err != nil {
		t.Fatalf("first AttemptIngest: %v", err)
	}
	if !inserted {
		t.Fatalf("first AttemptIngest: expected inserted=true, got false")
	}

	inserted2, err := s.AttemptIngest(ctx, repoID, sha, userID, week, 99, 99)
	if err != nil {
		t.Fatalf("second AttemptIngest: %v", err)
	}
	if inserted2 {
		t.Fatalf("second AttemptIngest: expected inserted=false on conflict, got true")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `TEST_DATABASE_DSN=postgres://... go test ./internal/store -run TestContributorStatsStore_AttemptIngest_DedupesBySha -v`
Expected: FAIL with `undefined: s.AttemptIngest` (compile error).

- [ ] **Step 3: Add the `errors` import**

`internal/store/contributor_stats_store.go` currently imports `context`, `database/sql`, `time`. Add `"errors"` to the import block so `AttemptIngest` can use `errors.Is(err, sql.ErrNoRows)`:

```go
import (
	"context"
	"database/sql"
	"errors"
	"time"
)
```

- [ ] **Step 4: Implement `AttemptIngest`**

Add to `internal/store/contributor_stats_store.go` after `AddDelta`:

```go
func (s *ContributorStatsStore) AttemptIngest(ctx context.Context, repoID int64, sha string, userID int64, week time.Time, additions, deletions int) (bool, error) {
	const q = `
		INSERT INTO contributor_commits_ingested (repo_id, sha, user_id, week, additions, deletions)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (repo_id, sha) DO NOTHING
		RETURNING 1
	`
	var one int
	err := s.db.QueryRowContext(ctx, q, repoID, sha, userID, MondayUTC(week), additions, deletions).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `TEST_DATABASE_DSN=postgres://... go test ./internal/store -run TestContributorStatsStore_AttemptIngest_DedupesBySha -v`
Expected: PASS. (Test SKIPs without `TEST_DATABASE_DSN` set — must be run locally against a real Postgres; CI without the DSN will skip silently.)

- [ ] **Step 6: Run the full store package tests**

Run: `TEST_DATABASE_DSN=postgres://... go test ./internal/store`
Expected: all PASS, including the existing `TestContributorStatsStore_UpsertAndList`.

- [ ] **Step 7: Commit**

```bash
git add internal/store/contributor_stats_store.go internal/store/contributor_stats_store_test.go
git commit -m "feat(store): AttemptIngest for atomic per-SHA contributor dedup"
```

---

## Task 3: `IngestCommit` gains `sha` and gates the increment

**Files:**
- Modify: `internal/service/contributor_stats_service.go`

This task changes the public signature of `ContributorStatsService.IngestCommit`. Task 4 updates the only caller.

- [ ] **Step 1: Verify the caller surface before changing the signature**

Run: `grep -rn "\.IngestCommit\b\|contributorStats\.IngestCommit\b" --include="*.go" internal/ cmd/`
Expected: exactly one call site, in `internal/service/repo_service.go` (inside `OnPostReceive`). If anything else appears (e.g. a CLI, a feature branch's new code path), stop and rescope — additional callers need parallel updates and the commit message changes.

- [ ] **Step 2: Update `IngestCommit` to claim-then-increment**

Replace the current `IngestCommit` body in `internal/service/contributor_stats_service.go`:

```go
func (s *ContributorStatsService) IngestCommit(ctx context.Context, repoID, userID int64, when time.Time, sha string, additions, deletions int) error {
	week := store.MondayUTC(when)
	claimed, err := s.stats.AttemptIngest(ctx, repoID, sha, userID, week, additions, deletions)
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	return s.stats.AddDelta(ctx, repoID, userID, week, 1, additions, deletions)
}
```

- [ ] **Step 3: Build the service package to cross-check Step 1**

Run: `go build ./internal/service/...`
Expected: FAIL with one error: `internal/service/repo_service.go:NNN: not enough arguments in call to s.contributorStats.IngestCommit`. Any additional errors mean Step 1's grep missed something.

- [ ] **Step 4: Do NOT commit yet**

Build is red. Continue to Task 4 and commit them together.

---

## Task 4: `OnPostReceive` passes `sha` through

**Files:**
- Modify: `internal/service/repo_service.go`

- [ ] **Step 1: Update the IngestCommit call site**

In `internal/service/repo_service.go`, inside the contributor-stats loop in `OnPostReceive` (currently around line 163), change:

```go
if err := s.contributorStats.IngestCommit(ctx, repo.ID, user.ID, c.AuthorTime,
    detail.TotalAdded, detail.TotalDeleted); err != nil {
    slog.Warn("post-receive: contributor stats ingest failed",
        "repo_id", repo.ID, "sha", c.SHA, "user_id", user.ID, "error", err)
}
```

to:

```go
if err := s.contributorStats.IngestCommit(ctx, repo.ID, user.ID, c.AuthorTime, c.SHA,
    detail.TotalAdded, detail.TotalDeleted); err != nil {
    slog.Warn("post-receive: contributor stats ingest failed",
        "repo_id", repo.ID, "sha", c.SHA, "user_id", user.ID, "error", err)
}
```

(`c.SHA` is already available on `postReceiveCommit`.)

- [ ] **Step 2: Build to confirm everything compiles**

Run: `go build ./...`
Expected: clean (no output).

- [ ] **Step 3: Run service + handler tests**

Run: `go test ./internal/service ./internal/handler`
Expected: all PASS.

- [ ] **Step 4: Commit Tasks 3 + 4 together**

```bash
git add internal/service/contributor_stats_service.go internal/service/repo_service.go
git commit -m "fix(stats): gate contributor IngestCommit on per-SHA dedup claim"
```

---

## Task 5: End-to-end double-ingest test

**Files:**
- Create: `internal/service/contributor_stats_service_test.go` (verified absent in the working tree)

The codebase convention is a per-file `openTestDB<Subject>Service` helper (see [`commit_stats_service_test.go:19`](../../../internal/service/commit_stats_service_test.go#L19)). This task creates a sibling helper named `openTestDBContributorStatsService`.

Pin the load-bearing behavior: two `IngestCommit` calls with the same SHA produce a single bucket increment, not two.

- [ ] **Step 1: Write the test file**

Create `internal/service/contributor_stats_service_test.go`:

```go
package service_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

func openTestDBContributorStatsService(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set; skipping integration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping test db: %v", err)
	}
	return db
}

func TestContributorStatsService_IngestCommit_IsIdempotentBySha(t *testing.T) {
	db := openTestDBContributorStatsService(t)
	defer db.Close()

	statsStore := store.NewContributorStatsStore(db)
	userStore := store.NewUserStore(db)
	svc := service.NewContributorStatsService(statsStore, userStore)

	ctx := context.Background()
	suffix := fmt.Sprintf("ccis_%d", os.Getpid())

	var userID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_superadmin) VALUES ($1, $2, 'x', false) RETURNING id`,
		suffix, suffix+"@test.invalid",
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var repoID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch) VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
		userID, suffix, "repo_"+suffix,
	).Scan(&repoID); err != nil {
		t.Fatalf("insert repo: %v", err)
	}
	t.Cleanup(func() {
		db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})

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
	if len(rows) != 1 {
		t.Fatalf("want 1 aggregate row, got %d: %+v", len(rows), rows)
	}
	if rows[0].Commits != 1 || rows[0].Additions != 50 || rows[0].Deletions != 10 {
		t.Errorf("idempotency violated: got commits=%d additions=%d deletions=%d, want 1/50/10",
			rows[0].Commits, rows[0].Additions, rows[0].Deletions)
	}
}
```

- [ ] **Step 2: Run the test against the real DB**

Run: `TEST_DATABASE_DSN=postgres://... go test ./internal/service -run TestContributorStatsService_IngestCommit_IsIdempotentBySha -v`
Expected: PASS. (Service implementation already lands in Task 3; this test confirms the contract.)

- [ ] **Step 3: Run the full test suite**

Run: `go test ./...`
Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/service/contributor_stats_service_test.go
git commit -m "test(stats): pin cross-call IngestCommit SHA idempotency"
```

---

## Out-of-scope follow-ups (do not implement here)

These are documented in the spec but explicitly out of scope for this plan:

1. **Backfill CLI** — `cloudzilla stats backfill-contributors` for repos where pre-migration drift is reported. Track separately; not required to ship this fix.
2. **Heatmap (`commit_day_counts`) SHA dedup** — same shape of problem, separate change.
3. **Reconciliation cron** — re-sums `contributor_commits_ingested` and reconciles against `contributor_week_stats` to correct the rare crash-between-writes case.

---

## Self-review

- **Spec coverage:** schema (Task 1), `AttemptIngest` store API (Task 2), `IngestCommit` claim-and-increment service (Task 3), caller plumbing (Task 4), and idempotency assertion (Task 5) all map back to spec sections. The three out-of-scope items in the spec are listed above, not silently dropped.
- **Placeholders:** none — every step has the actual code or command.
- **Type consistency:** `AttemptIngest(ctx, repoID, sha, userID, week, additions, deletions) (bool, error)` is identical in Task 2's definition and Task 3's call. `IngestCommit(ctx, repoID, userID, when, sha, additions, deletions)` is identical in Task 3's definition and Task 4's call.
- **Atomicity caveat:** the non-transactional two-write trade-off is called out in the spec; the plan does not paper over it. The integration test in Task 5 covers the happy path, not the crash-window case (which is what reconciliation is for, out of scope).
