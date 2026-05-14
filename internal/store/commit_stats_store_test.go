package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// openTestDBCommitStats opens an integration-test DB connection if
// TEST_DATABASE_DSN is set; otherwise it skips the test. Mirrors the
// helper used by issue_store_visibility_test.go but uses a unique name
// so both files can live in the same test package.
func openTestDBCommitStats(t *testing.T) *sql.DB {
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

func TestCommitStatsStore_UpsertAndListForUser(t *testing.T) {
	db := openTestDBCommitStats(t)
	defer db.Close()

	ctx := context.Background()

	// Unique suffix avoids collisions across parallel test runs.
	suffix := fmt.Sprintf("%d", os.Getpid())

	// Seed: one user.
	var userID int64
	err := db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_superadmin)
		 VALUES ($1, $2, 'x', false) RETURNING id`,
		"commitstats_"+suffix, "commitstats_"+suffix+"@test.invalid",
	).Scan(&userID)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	// Seed: one repository owned by that user.
	var repoID int64
	err = db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
		 VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
		userID, "commitstats_"+suffix, "repo_"+suffix,
	).Scan(&repoID)
	if err != nil {
		t.Fatalf("insert repo: %v", err)
	}

	// Clean up via the user cascade — commit_day_counts has
	// ON DELETE CASCADE on both repo_id and user_id, and repositories
	// cascades from users, so a single DELETE FROM users tears it all down.
	t.Cleanup(func() {
		bg := context.Background()
		db.ExecContext(bg, `DELETE FROM users WHERE id = $1`, userID)
	})

	today := time.Now().UTC().Truncate(24 * time.Hour)
	yesterday := today.AddDate(0, 0, -1)

	s := store.NewCommitStatsStore(db)

	// First write for (repo, user, today) — 3 commits.
	if err := s.UpsertCount(ctx, repoID, userID, today, 3); err != nil {
		t.Fatalf("upsert today=3: %v", err)
	}
	// Yesterday — 5 commits.
	if err := s.UpsertCount(ctx, repoID, userID, yesterday, 5); err != nil {
		t.Fatalf("upsert yesterday=5: %v", err)
	}
	// Re-upsert today — must REPLACE (not add to) the existing row.
	if err := s.UpsertCount(ctx, repoID, userID, today, 4); err != nil {
		t.Fatalf("upsert today=4 (replace): %v", err)
	}

	rows, err := s.ListForUserSince(ctx, userID, yesterday)
	if err != nil {
		t.Fatalf("list for user: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d (%+v)", len(rows), rows)
	}

	// Find today's row and assert count == 4 (replace, not add to 3+4=7).
	var todayCount, yesterdayCount int
	for _, r := range rows {
		switch r.Day.UTC().Truncate(24 * time.Hour) {
		case today:
			todayCount = r.CommitCount
		case yesterday:
			yesterdayCount = r.CommitCount
		}
	}
	if todayCount != 4 {
		t.Errorf("today commit_count: want 4 (replace), got %d", todayCount)
	}
	if yesterdayCount != 5 {
		t.Errorf("yesterday commit_count: want 5, got %d", yesterdayCount)
	}
}

// AddCount must accumulate across calls so successive pushes on a single day
// don't overwrite one another (the bug fixed alongside #8).
func TestCommitStatsStore_AddCount_Additive(t *testing.T) {
	db := openTestDBCommitStats(t)
	defer db.Close()

	ctx := context.Background()
	suffix := fmt.Sprintf("addct_%d", os.Getpid())

	var userID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_superadmin)
		 VALUES ($1, $2, 'x', false) RETURNING id`,
		suffix, suffix+"@test.invalid",
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var repoID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
		 VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
		userID, suffix, "repo_"+suffix,
	).Scan(&repoID); err != nil {
		t.Fatalf("insert repo: %v", err)
	}
	t.Cleanup(func() {
		db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})

	s := store.NewCommitStatsStore(db)
	today := time.Now().UTC().Truncate(24 * time.Hour)

	if err := s.AddCount(ctx, repoID, userID, today, 3); err != nil {
		t.Fatalf("add 3: %v", err)
	}
	if err := s.AddCount(ctx, repoID, userID, today, 4); err != nil {
		t.Fatalf("add 4: %v", err)
	}
	rows, err := s.ListForUserSince(ctx, userID, today)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d (%+v)", len(rows), rows)
	}
	if rows[0].CommitCount != 7 {
		t.Errorf("AddCount must accumulate; want 7 (3+4), got %d", rows[0].CommitCount)
	}

	has, err := s.HasRowsForRepoSince(ctx, repoID, today)
	if err != nil {
		t.Fatalf("has rows: %v", err)
	}
	if !has {
		t.Errorf("HasRowsForRepoSince must return true after AddCount")
	}

	// Soft-deleted repos must drop out of the user heatmap query so the
	// contribution grid stops counting work in deleted repositories.
	if _, err := db.ExecContext(ctx,
		`UPDATE repositories SET deleted_at = NOW() WHERE id = $1`, repoID,
	); err != nil {
		t.Fatalf("soft-delete repo: %v", err)
	}
	rows, err = s.ListForUserSince(ctx, userID, today)
	if err != nil {
		t.Fatalf("list after soft-delete: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("ListForUserSince must skip soft-deleted repos; want 0 rows, got %d (%+v)", len(rows), rows)
	}
}
