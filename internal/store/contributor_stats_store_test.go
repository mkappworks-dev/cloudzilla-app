package store_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

func TestContributorStatsStore_UpsertAndList(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set; skipping integration test")
	}
	db := openTestDBCommitStats(t)
	defer db.Close()

	s := store.NewContributorStatsStore(db)
	ctx := context.Background()
	suffix := fmt.Sprintf("cws_%d", os.Getpid())

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

	week := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC) // Monday
	if err := s.UpsertStats(ctx, repoID, userID, week, 3, 100, 20); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// Idempotent re-upsert with new values — must replace, not accumulate.
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
