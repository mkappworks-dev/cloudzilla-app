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

// openTestDBCommitStatsService opens an integration-test DB connection if
// TEST_DATABASE_DSN is set; otherwise it skips the test. Mirrors the helper
// used by other integration tests.
func openTestDBCommitStatsService(t *testing.T) *sql.DB {
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

func TestCommitStatsService_Ingest_AggregatesPerDay(t *testing.T) {
	db := openTestDBCommitStatsService(t)
	defer db.Close()

	ctx := context.Background()

	// Unique suffix avoids collisions across parallel test runs.
	suffix := fmt.Sprintf("%d", os.Getpid())

	// Seed: one user.
	var userID int64
	email := "commitstatssvc_" + suffix + "@test.invalid"
	err := db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_superadmin)
		 VALUES ($1, $2, 'x', false) RETURNING id`,
		"commitstatssvc_"+suffix, email,
	).Scan(&userID)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	// Seed: one repository owned by that user.
	var repoID int64
	err = db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
		 VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
		userID, "commitstatssvc_"+suffix, "repo_"+suffix,
	).Scan(&repoID)
	if err != nil {
		t.Fatalf("insert repo: %v", err)
	}

	// Cleanup via user cascade.
	t.Cleanup(func() {
		bg := context.Background()
		db.ExecContext(bg, `DELETE FROM users WHERE id = $1`, userID)
	})

	users := store.NewUserStore(db)
	statsStore := store.NewCommitStatsStore(db)
	svc := service.NewCommitStatsService(statsStore, users)

	now := time.Now().UTC().Truncate(24 * time.Hour)
	dayMinus2 := now.AddDate(0, 0, -2)
	dayMinus1 := now.AddDate(0, 0, -1)

	// 2 commits on day -2, 1 on day -1, all attributed to the seeded user.
	samples := []service.CommitSample{
		{AuthorEmail: email, Time: dayMinus2.Add(3 * time.Hour)},
		{AuthorEmail: email, Time: dayMinus2.Add(7 * time.Hour)},
		{AuthorEmail: email, Time: dayMinus1.Add(2 * time.Hour)},
	}
	if err := svc.Ingest(ctx, repoID, samples); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	rows, err := statsStore.ListForUserSince(ctx, userID, now.AddDate(0, 0, -3))
	if err != nil {
		t.Fatalf("list for user: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d (%+v)", len(rows), rows)
	}
	var d2, d1 int
	for _, r := range rows {
		switch r.Day.UTC().Truncate(24 * time.Hour) {
		case dayMinus2:
			d2 = r.CommitCount
		case dayMinus1:
			d1 = r.CommitCount
		}
	}
	if d2 != 2 {
		t.Errorf("day -2 count: want 2, got %d", d2)
	}
	if d1 != 1 {
		t.Errorf("day -1 count: want 1, got %d", d1)
	}

	// Anonymous-commit skip: ingest a sample whose email doesn't match any
	// user. Existing rows must remain unchanged and no error returned.
	if err := svc.Ingest(ctx, repoID, []service.CommitSample{
		{AuthorEmail: "ghost@example.invalid", Time: now.Add(time.Hour)},
	}); err != nil {
		t.Fatalf("ingest anonymous: %v", err)
	}
	rows, err = statsStore.ListForUserSince(ctx, userID, now.AddDate(0, 0, -3))
	if err != nil {
		t.Fatalf("list for user after anon: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("anonymous ingest must not add rows; want 2, got %d", len(rows))
	}

	// LookbackForUser shape: full window materialized as zeros except the
	// days we ingested.
	const lookback = 30
	out, err := svc.LookbackForUser(ctx, userID, lookback)
	if err != nil {
		t.Fatalf("lookback: %v", err)
	}
	if len(out) != lookback {
		t.Fatalf("lookback map size: want %d, got %d", lookback, len(out))
	}
	if got := out[dayMinus2]; got != 2 {
		t.Errorf("lookback day -2: want 2, got %d", got)
	}
	if got := out[dayMinus1]; got != 1 {
		t.Errorf("lookback day -1: want 1, got %d", got)
	}
	// Today wasn't ingested → must be present as zero.
	if got, ok := out[now]; !ok || got != 0 {
		t.Errorf("lookback today: want present=true count=0, got present=%v count=%d", ok, got)
	}
}
