package service_test

// Integration test for CommitStatsService.CommitsForUserSince.
// Seeds three days of upserts (commit_day_counts) within the lookback
// window plus one stale row outside it, then asserts the total matches
// the in-window sum.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

func openCommitsForUserSinceTestDB(t *testing.T) *sql.DB {
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

func TestCommitStatsService_CommitsForUserSince(t *testing.T) {
	db := openCommitsForUserSinceTestDB(t)
	defer db.Close()

	ctx := context.Background()
	suffix := fmt.Sprintf("%d", os.Getpid())

	var aliceID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_superadmin)
		 VALUES ($1, $2, 'x', false) RETURNING id`,
		"cforu_"+suffix, "cforu_"+suffix+"@test.invalid",
	).Scan(&aliceID); err != nil {
		t.Fatalf("insert alice: %v", err)
	}
	defer db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, aliceID)

	var repoID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
		 VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
		aliceID, "cforu_"+suffix, "repo_"+suffix,
	).Scan(&repoID); err != nil {
		t.Fatalf("insert repo: %v", err)
	}

	statsStore := store.NewCommitStatsStore(db)
	today := time.Now().UTC().Truncate(24 * time.Hour)

	// Three in-window days within the last 7: today=2, yesterday=3, 6 days ago=5 → total 10.
	for offset, count := range map[int]int{0: 2, 1: 3, 6: 5} {
		if err := statsStore.UpsertCount(ctx, repoID, aliceID, today.AddDate(0, 0, -offset), count); err != nil {
			t.Fatalf("upsert offset %d: %v", offset, err)
		}
	}
	// Stale row 30 days back — should not contribute when days=7.
	if err := statsStore.UpsertCount(ctx, repoID, aliceID, today.AddDate(0, 0, -30), 100); err != nil {
		t.Fatalf("upsert stale: %v", err)
	}

	users := store.NewUserStore(db)
	svc := service.NewCommitStatsService(statsStore, users)

	got, err := svc.CommitsForUserSince(ctx, aliceID, 7)
	if err != nil {
		t.Fatalf("CommitsForUserSince: %v", err)
	}
	if got != 10 {
		t.Errorf("CommitsForUserSince(7d): want 10, got %d", got)
	}
}
