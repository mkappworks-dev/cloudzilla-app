package service_test

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
	t.Cleanup(func() {
		db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})

	var repoID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch) VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
		userID, suffix, "repo_"+suffix,
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
	if len(rows) != 1 {
		t.Fatalf("want 1 aggregate row, got %d: %+v", len(rows), rows)
	}
	if rows[0].Commits != 1 || rows[0].Additions != 50 || rows[0].Deletions != 10 {
		t.Errorf("idempotency violated: got commits=%d additions=%d deletions=%d, want 1/50/10",
			rows[0].Commits, rows[0].Additions, rows[0].Deletions)
	}
}
