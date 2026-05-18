package store_test

// Integration tests for the account count helpers; skipped when TEST_DATABASE_DSN is unset.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

func seedTwoUsers(t *testing.T, ctx context.Context, prefix string) (aliceID, bobID int64, cleanup func()) {
	t.Helper()
	suffix := fmt.Sprintf("%s_%d_%s", prefix, os.Getpid(), t.Name())
	suffix = strings.NewReplacer("/", "_", " ", "_").Replace(suffix)
	db := openTestDB(t)
	t.Cleanup(func() { db.Close() })

	if err := db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_superadmin)
		 VALUES ($1, $2, 'x', false) RETURNING id`,
		"alice_"+suffix, "alice_"+suffix+"@test.invalid",
	).Scan(&aliceID); err != nil {
		t.Fatalf("insert alice: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_superadmin)
		 VALUES ($1, $2, 'x', false) RETURNING id`,
		"bob_"+suffix, "bob_"+suffix+"@test.invalid",
	).Scan(&bobID); err != nil {
		t.Fatalf("insert bob: %v", err)
	}
	cleanup = func() {
		bg := context.Background()
		db.ExecContext(bg, `DELETE FROM users WHERE id IN ($1, $2)`, aliceID, bobID)
	}
	return aliceID, bobID, cleanup
}

func TestRepoStore_CountForUser(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	aliceID, bobID, cleanup := seedTwoUsers(t, ctx, "repocnt")
	defer cleanup()

	suffix := fmt.Sprintf("%d", os.Getpid())

	// alice owns 2 live repos + 1 soft-deleted; bob owns 1.
	for i, name := range []string{"a1", "a2"} {
		_, err := db.ExecContext(ctx,
			`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
			 VALUES ($1, $2, $3, '', false, 'main')`,
			aliceID, "alice_"+suffix, fmt.Sprintf("%s_%d_%s", name, i, suffix),
		)
		if err != nil {
			t.Fatalf("insert alice repo %s: %v", name, err)
		}
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch, deleted_at)
		 VALUES ($1, $2, $3, '', false, 'main', NOW())`,
		aliceID, "alice_"+suffix, "a3_deleted_"+suffix,
	); err != nil {
		t.Fatalf("insert alice deleted repo: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
		 VALUES ($1, $2, $3, '', false, 'main')`,
		bobID, "bob_"+suffix, "b1_"+suffix,
	); err != nil {
		t.Fatalf("insert bob repo: %v", err)
	}

	s := store.NewRepoStore(db)

	got, err := s.CountForUser(ctx, aliceID)
	if err != nil {
		t.Fatalf("CountForUser(alice): %v", err)
	}
	if got != 2 {
		t.Errorf("CountForUser(alice): want 2 (deleted excluded), got %d", got)
	}

	got, err = s.CountForUser(ctx, bobID)
	if err != nil {
		t.Fatalf("CountForUser(bob): %v", err)
	}
	if got != 1 {
		t.Errorf("CountForUser(bob): want 1, got %d", got)
	}
}

func TestPullStore_CountOpenAssignedTo(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	aliceID, bobID, cleanup := seedTwoUsers(t, ctx, "pullassigned")
	defer cleanup()

	suffix := fmt.Sprintf("%d", os.Getpid())

	var repoID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
		 VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
		aliceID, "alice_"+suffix, "pullassignedrepo_"+suffix,
	).Scan(&repoID); err != nil {
		t.Fatalf("insert repo: %v", err)
	}

	// alice authors every PR but is assigned to none.
	var openPR, closedPR int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO pull_requests (repo_id, number, author_id, title, state, head_branch, base_branch)
		 VALUES ($1, 1, $2, 't1', 'open', 'h1', 'main') RETURNING id`,
		repoID, aliceID,
	).Scan(&openPR); err != nil {
		t.Fatalf("insert open pr: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO pull_requests (repo_id, number, author_id, title, state, head_branch, base_branch)
		 VALUES ($1, 2, $2, 't2', 'open', 'h2', 'main')`,
		repoID, aliceID,
	); err != nil {
		t.Fatalf("insert open pr2: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`INSERT INTO pull_requests (repo_id, number, author_id, title, state, head_branch, base_branch)
		 VALUES ($1, 3, $2, 't3-closed', 'closed', 'h3', 'main') RETURNING id`,
		repoID, aliceID,
	).Scan(&closedPR); err != nil {
		t.Fatalf("insert closed pr: %v", err)
	}

	// bob is assigned to one open PR and one closed PR.
	for _, id := range []int64{openPR, closedPR} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO pull_assignees (pull_id, user_id) VALUES ($1, $2)`,
			id, bobID,
		); err != nil {
			t.Fatalf("insert pull assignee: %v", err)
		}
	}

	s := store.NewPullStore(db)

	got, err := s.CountOpenAssignedTo(ctx, aliceID)
	if err != nil {
		t.Fatalf("CountOpenAssignedTo(alice): %v", err)
	}
	if got != 0 {
		t.Errorf("alice: want 0 (authoring a PR does not make her an assignee), got %d", got)
	}

	got, err = s.CountOpenAssignedTo(ctx, bobID)
	if err != nil {
		t.Fatalf("CountOpenAssignedTo(bob): %v", err)
	}
	if got != 1 {
		t.Errorf("bob: want 1 (open assigned only, closed excluded), got %d", got)
	}
}

func TestIssueStore_CountOpenAssignedTo(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	aliceID, bobID, cleanup := seedTwoUsers(t, ctx, "issueassigned")
	defer cleanup()

	suffix := fmt.Sprintf("%d", os.Getpid())

	var repoID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
		 VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
		aliceID, "alice_"+suffix, "issueassignedrepo_"+suffix,
	).Scan(&repoID); err != nil {
		t.Fatalf("insert repo: %v", err)
	}

	// alice authors every issue but is assigned to none.
	var open1, closed3 int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO issues (repo_id, number, author_id, title, body, state)
		 VALUES ($1, 1, $2, 't1', '', 'open') RETURNING id`,
		repoID, aliceID,
	).Scan(&open1); err != nil {
		t.Fatalf("insert issue1: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO issues (repo_id, number, author_id, title, body, state)
		 VALUES ($1, 2, $2, 't2', '', 'open')`,
		repoID, aliceID,
	); err != nil {
		t.Fatalf("insert issue2: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`INSERT INTO issues (repo_id, number, author_id, title, body, state)
		 VALUES ($1, 3, $2, 't3-closed', '', 'closed') RETURNING id`,
		repoID, aliceID,
	).Scan(&closed3); err != nil {
		t.Fatalf("insert issue3: %v", err)
	}

	// bob is assigned to one open issue and one closed issue.
	for _, id := range []int64{open1, closed3} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO issue_assignees (issue_id, user_id) VALUES ($1, $2)`,
			id, bobID,
		); err != nil {
			t.Fatalf("insert issue assignee: %v", err)
		}
	}

	s := store.NewIssueStore(db)

	got, err := s.CountOpenAssignedTo(ctx, aliceID)
	if err != nil {
		t.Fatalf("CountOpenAssignedTo(alice): %v", err)
	}
	if got != 0 {
		t.Errorf("alice: want 0 (authoring an issue does not make her an assignee), got %d", got)
	}

	got, err = s.CountOpenAssignedTo(ctx, bobID)
	if err != nil {
		t.Fatalf("CountOpenAssignedTo(bob): %v", err)
	}
	if got != 1 {
		t.Errorf("bob: want 1 (open assigned only, closed excluded), got %d", got)
	}
}
