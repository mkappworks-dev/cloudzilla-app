package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

func openTestDB(t *testing.T) *sql.DB {
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

func TestListIssues_VisibilityFilter(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// Use a unique suffix to avoid collisions across test runs.
	suffix := fmt.Sprintf("%d", os.Getpid())

	// Create owner user.
	var ownerID int64
	err := db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_superadmin)
		 VALUES ($1, $2, 'x', false) RETURNING id`,
		"testowner_"+suffix, "owner_"+suffix+"@test.invalid",
	).Scan(&ownerID)
	if err != nil {
		t.Fatalf("insert owner: %v", err)
	}

	// Create non-member user.
	var nonMemberID int64
	err = db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_superadmin)
		 VALUES ($1, $2, 'x', false) RETURNING id`,
		"nonmember_"+suffix, "nonmember_"+suffix+"@test.invalid",
	).Scan(&nonMemberID)
	if err != nil {
		t.Fatalf("insert non-member: %v", err)
	}

	// Create repo owned by ownerID.
	var repoID int64
	err = db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private)
		 VALUES ($1, $2, $3, '', false) RETURNING id`,
		ownerID, "testowner_"+suffix, "testrepo_"+suffix,
	).Scan(&repoID)
	if err != nil {
		t.Fatalf("insert repo: %v", err)
	}

	// Give owner a permissions row.
	_, err = db.ExecContext(ctx,
		`INSERT INTO permissions (user_id, repo_id, role) VALUES ($1, $2, 'owner')`,
		ownerID, repoID,
	)
	if err != nil {
		t.Fatalf("insert permission: %v", err)
	}

	// Insert public issue.
	var pubIssueID int64
	err = db.QueryRowContext(ctx,
		`INSERT INTO issues (repo_id, number, author_id, title, body, state, visibility)
		 VALUES ($1, 1, $2, 'Public Issue', '', 'open', 'public') RETURNING id`,
		repoID, ownerID,
	).Scan(&pubIssueID)
	if err != nil {
		t.Fatalf("insert public issue: %v", err)
	}

	// Insert private issue authored by owner.
	var privIssueID int64
	err = db.QueryRowContext(ctx,
		`INSERT INTO issues (repo_id, number, author_id, title, body, state, visibility)
		 VALUES ($1, 2, $2, 'Private Issue', '', 'open', 'private') RETURNING id`,
		repoID, ownerID,
	).Scan(&privIssueID)
	if err != nil {
		t.Fatalf("insert private issue: %v", err)
	}

	issueStore := store.NewIssueStore(db)

	// Anonymous caller — only public visible.
	anonIssues, err := issueStore.ListByRepo(ctx, repoID, nil, nil, 1, 50)
	if err != nil {
		t.Fatalf("ListByRepo (anon): %v", err)
	}
	if len(anonIssues) != 1 {
		t.Errorf("anon: want 1 issue, got %d", len(anonIssues))
	}
	if len(anonIssues) > 0 && anonIssues[0].Visibility != "public" {
		t.Errorf("anon: expected public issue, got visibility=%q", anonIssues[0].Visibility)
	}

	// Owner — sees both.
	ownerIssues, err := issueStore.ListByRepo(ctx, repoID, nil, &ownerID, 1, 50)
	if err != nil {
		t.Fatalf("ListByRepo (owner): %v", err)
	}
	if len(ownerIssues) != 2 {
		t.Errorf("owner: want 2 issues, got %d", len(ownerIssues))
	}

	// Cleanup.
	db.ExecContext(ctx, `DELETE FROM issues WHERE repo_id = $1`, repoID)
	db.ExecContext(ctx, `DELETE FROM permissions WHERE repo_id = $1`, repoID)
	db.ExecContext(ctx, `DELETE FROM repositories WHERE id = $1`, repoID)
	db.ExecContext(ctx, `DELETE FROM users WHERE id IN ($1, $2)`, ownerID, nonMemberID)
}
