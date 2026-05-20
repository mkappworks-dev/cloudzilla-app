package service_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

func openUserPinTestDB(t *testing.T) *sql.DB {
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

func TestUserService_PinUnpin(t *testing.T) {
	db := openUserPinTestDB(t)
	defer db.Close()

	ctx := context.Background()
	suffix := fmt.Sprintf("%d", os.Getpid())

	var userID int64
	err := db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_superadmin)
		 VALUES ($1, $2, 'x', false) RETURNING id`,
		"pinuser_"+suffix, "pinuser_"+suffix+"@test.invalid",
	).Scan(&userID)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	defer db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID)

	// Seed 7 repos to exercise the pin limit.
	repoIDs := make([]int64, 7)
	for i := 0; i < 7; i++ {
		err := db.QueryRowContext(ctx,
			`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
			 VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
			userID, "pinuser_"+suffix, fmt.Sprintf("pinrepo_%s_%d", suffix, i),
		).Scan(&repoIDs[i])
		if err != nil {
			t.Fatalf("insert repo %d: %v", i, err)
		}
	}

	userStore := store.NewUserStore(db)
	svc := service.NewUserService(userStore, config.AuthConfig{})

	// Pin r1, pin r2 -> [r1, r2].
	if err := svc.PinRepo(ctx, userID, repoIDs[0]); err != nil {
		t.Fatalf("pin r1: %v", err)
	}
	if err := svc.PinRepo(ctx, userID, repoIDs[1]); err != nil {
		t.Fatalf("pin r2: %v", err)
	}
	got, err := svc.PinnedRepoIDs(ctx, userID)
	if err != nil {
		t.Fatalf("list after 2 pins: %v", err)
	}
	if len(got) != 2 || got[0] != repoIDs[0] || got[1] != repoIDs[1] {
		t.Fatalf("after pin r1,r2: want [%d,%d], got %v", repoIDs[0], repoIDs[1], got)
	}

	// Idempotent: pinning r1 again is a no-op.
	if err := svc.PinRepo(ctx, userID, repoIDs[0]); err != nil {
		t.Fatalf("re-pin r1: %v", err)
	}
	got, err = svc.PinnedRepoIDs(ctx, userID)
	if err != nil {
		t.Fatalf("list after re-pin: %v", err)
	}
	if len(got) != 2 || got[0] != repoIDs[0] || got[1] != repoIDs[1] {
		t.Fatalf("after re-pin r1: want [%d,%d], got %v", repoIDs[0], repoIDs[1], got)
	}

	// Unpin r1 -> [r2].
	if err := svc.UnpinRepo(ctx, userID, repoIDs[0]); err != nil {
		t.Fatalf("unpin r1: %v", err)
	}
	got, err = svc.PinnedRepoIDs(ctx, userID)
	if err != nil {
		t.Fatalf("list after unpin: %v", err)
	}
	if len(got) != 1 || got[0] != repoIDs[1] {
		t.Fatalf("after unpin r1: want [%d], got %v", repoIDs[1], got)
	}

	// Pin up to 6, then 7th must fail.
	// Currently pinned: [r2]. Add r1..r6 from the unused slice to reach 6 total.
	toFill := []int64{repoIDs[0], repoIDs[2], repoIDs[3], repoIDs[4], repoIDs[5]}
	for i, id := range toFill {
		if err := svc.PinRepo(ctx, userID, id); err != nil {
			t.Fatalf("fill pin %d: %v", i, err)
		}
	}
	got, err = svc.PinnedRepoIDs(ctx, userID)
	if err != nil {
		t.Fatalf("list after filling: %v", err)
	}
	if len(got) != 6 {
		t.Fatalf("want 6 pinned, got %d (%v)", len(got), got)
	}

	// 7th pin must return ErrPinLimit.
	err = svc.PinRepo(ctx, userID, repoIDs[6])
	if !errors.Is(err, service.ErrPinLimit) {
		t.Fatalf("7th pin: want ErrPinLimit, got %v", err)
	}
}
