package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestContributorStatsStore_UpsertAndList(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set; skipping integration test")
	}
	db := testutil.OpenTestDB(t)

	s := store.NewContributorStatsStore(db)
	ctx := context.Background()
	suffix := "cws_" + testutil.UniqueSuffix(t)

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
		testutil.Exec(t, db, `DELETE FROM users WHERE id = $1`, userID)
	})

	week := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	if err := s.UpsertStats(ctx, repoID, userID, week, 3, 100, 20); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := s.UpsertStats(ctx, repoID, userID, week, 5, 150, 30); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	rows, err := s.ListForRepo(ctx, repoID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Commits != 5 {
		t.Errorf("expected 1 row with 5 commits, got %+v", rows)
	}
}

func TestContributorStatsStore_IngestCommitTx_DedupesBothAggregates(t *testing.T) {
	if os.Getenv("TEST_DATABASE_DSN") == "" {
		t.Skip("TEST_DATABASE_DSN not set; skipping integration test")
	}
	db := testutil.OpenTestDB(t)

	s := store.NewContributorStatsStore(db)
	ctx := context.Background()
	suffix := "cci_" + testutil.UniqueSuffix(t)

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
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
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
