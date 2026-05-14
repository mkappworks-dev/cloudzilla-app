package service_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

func openAttentionTestDB(t *testing.T) *sql.DB {
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

// TestAttentionService_ForUser verifies that ForUser returns open issues
// assigned to the user that the user did NOT author, filtering out
// self-authored assignments.
func TestAttentionService_ForUser(t *testing.T) {
	db := openAttentionTestDB(t)
	defer db.Close()

	ctx := context.Background()
	suffix := fmt.Sprintf("%d", os.Getpid())

	// Seed alice (assignee/viewer).
	var aliceID int64
	err := db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_superadmin)
		 VALUES ($1, $2, 'x', false) RETURNING id`,
		"alice_"+suffix, "alice_"+suffix+"@test.invalid",
	).Scan(&aliceID)
	if err != nil {
		t.Fatalf("insert alice: %v", err)
	}

	// Seed bob (repo owner + author of issue #1).
	var bobID int64
	err = db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_superadmin)
		 VALUES ($1, $2, 'x', false) RETURNING id`,
		"bob_"+suffix, "bob_"+suffix+"@test.invalid",
	).Scan(&bobID)
	if err != nil {
		t.Fatalf("insert bob: %v", err)
	}

	// Defer cleanup; cascades delete issues + issue_assignees + repositories.
	defer func() {
		db.ExecContext(ctx, `DELETE FROM users WHERE id IN ($1, $2)`, aliceID, bobID)
	}()

	// Seed repo owned by bob.
	var repoID int64
	err = db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
		 VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
		bobID, "bob_"+suffix, "attentionrepo_"+suffix,
	).Scan(&repoID)
	if err != nil {
		t.Fatalf("insert repo: %v", err)
	}

	// Issue #1 — authored by bob, assigned to alice (should appear).
	var issue1ID int64
	err = db.QueryRowContext(ctx,
		`INSERT INTO issues (repo_id, number, author_id, title, body, state)
		 VALUES ($1, 1, $2, $3, '', 'open') RETURNING id`,
		repoID, bobID, "Issue from bob to alice",
	).Scan(&issue1ID)
	if err != nil {
		t.Fatalf("insert issue1: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO issue_assignees (issue_id, user_id) VALUES ($1, $2)`,
		issue1ID, aliceID,
	); err != nil {
		t.Fatalf("insert issue1 assignee: %v", err)
	}

	// Issue #2 — authored by alice, assigned to alice (should be filtered).
	var issue2ID int64
	err = db.QueryRowContext(ctx,
		`INSERT INTO issues (repo_id, number, author_id, title, body, state)
		 VALUES ($1, 2, $2, $3, '', 'open') RETURNING id`,
		repoID, aliceID, "Issue alice authored and self-assigned",
	).Scan(&issue2ID)
	if err != nil {
		t.Fatalf("insert issue2: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO issue_assignees (issue_id, user_id) VALUES ($1, $2)`,
		issue2ID, aliceID,
	); err != nil {
		t.Fatalf("insert issue2 assignee: %v", err)
	}

	issueStore := store.NewIssueStore(db)
	svc := service.NewAttentionService(issueStore)

	items, err := svc.ForUser(ctx, aliceID)
	if err != nil {
		t.Fatalf("ForUser: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 attention item, got %d (%+v)", len(items), items)
	}
	if items[0].Kind != service.AttentionIssueAssigned {
		t.Errorf("kind: want %q, got %q", service.AttentionIssueAssigned, items[0].Kind)
	}
	if items[0].RefID != issue1ID {
		t.Errorf("RefID: want %d, got %d", issue1ID, items[0].RefID)
	}
}
