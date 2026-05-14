package store_test

// Integration tests for the home page count helpers:
//   - RepoStore.CountForUser
//   - PullStore.CountOpenAuthoredByOrAssignedTo
//   - IssueStore.CountOpenAuthoredByOrAssignedTo
//
// Each test seeds two users, one repo, and the minimum fixture rows
// needed to exercise the SQL. Tests are skipped when TEST_DATABASE_DSN
// is unset, matching the project's existing integration-test pattern.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// seedTwoUsers inserts two test users and returns their IDs plus a cleanup
// func that deletes them (cascading to repositories, issues, pulls, …).
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

func TestPullStore_CountOpenAuthoredByOrAssignedTo(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	aliceID, bobID, cleanup := seedTwoUsers(t, ctx, "pullcnt")
	defer cleanup()

	suffix := fmt.Sprintf("%d", os.Getpid())

	// repo owned by alice
	var repoID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
		 VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
		aliceID, "alice_"+suffix, "pullcntrepo_"+suffix,
	).Scan(&repoID); err != nil {
		t.Fatalf("insert repo: %v", err)
	}

	// 2 open PRs authored by alice, 1 closed authored by alice.
	var openPR1, openPR2 int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO pull_requests (repo_id, number, author_id, title, state, head_branch, base_branch)
		 VALUES ($1, 1, $2, 't1', 'open', 'h1', 'main') RETURNING id`,
		repoID, aliceID,
	).Scan(&openPR1); err != nil {
		t.Fatalf("insert open pr1: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`INSERT INTO pull_requests (repo_id, number, author_id, title, state, head_branch, base_branch)
		 VALUES ($1, 2, $2, 't2', 'open', 'h2', 'main') RETURNING id`,
		repoID, aliceID,
	).Scan(&openPR2); err != nil {
		t.Fatalf("insert open pr2: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO pull_requests (repo_id, number, author_id, title, state, head_branch, base_branch)
		 VALUES ($1, 3, $2, 't3-closed', 'closed', 'h3', 'main')`,
		repoID, aliceID,
	); err != nil {
		t.Fatalf("insert closed pr: %v", err)
	}

	// bob assigned to openPR1.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO pull_assignees (pull_id, user_id) VALUES ($1, $2)`,
		openPR1, bobID,
	); err != nil {
		t.Fatalf("insert pull assignee: %v", err)
	}

	s := store.NewPullStore(db)

	got, err := s.CountOpenAuthoredByOrAssignedTo(ctx, aliceID)
	if err != nil {
		t.Fatalf("CountOpenAuthoredByOrAssignedTo(alice): %v", err)
	}
	if got != 2 {
		t.Errorf("alice: want 2 (own opens, dedup), got %d", got)
	}

	got, err = s.CountOpenAuthoredByOrAssignedTo(ctx, bobID)
	if err != nil {
		t.Fatalf("CountOpenAuthoredByOrAssignedTo(bob): %v", err)
	}
	if got != 1 {
		t.Errorf("bob: want 1 (assignee only), got %d", got)
	}
}

func TestIssueStore_CountOpenAuthoredByOrAssignedTo(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	aliceID, bobID, cleanup := seedTwoUsers(t, ctx, "issuecnt")
	defer cleanup()

	suffix := fmt.Sprintf("%d", os.Getpid())

	var repoID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
		 VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
		aliceID, "alice_"+suffix, "issuecntrepo_"+suffix,
	).Scan(&repoID); err != nil {
		t.Fatalf("insert repo: %v", err)
	}

	// 2 open issues by alice + 1 closed by alice.
	var i1 int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO issues (repo_id, number, author_id, title, body, state)
		 VALUES ($1, 1, $2, 't1', '', 'open') RETURNING id`,
		repoID, aliceID,
	).Scan(&i1); err != nil {
		t.Fatalf("insert issue1: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO issues (repo_id, number, author_id, title, body, state)
		 VALUES ($1, 2, $2, 't2', '', 'open')`,
		repoID, aliceID,
	); err != nil {
		t.Fatalf("insert issue2: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO issues (repo_id, number, author_id, title, body, state)
		 VALUES ($1, 3, $2, 't3-closed', '', 'closed')`,
		repoID, aliceID,
	); err != nil {
		t.Fatalf("insert issue3: %v", err)
	}

	// bob is assigned to issue1.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO issue_assignees (issue_id, user_id) VALUES ($1, $2)`,
		i1, bobID,
	); err != nil {
		t.Fatalf("insert issue assignee: %v", err)
	}

	s := store.NewIssueStore(db)

	got, err := s.CountOpenAuthoredByOrAssignedTo(ctx, aliceID)
	if err != nil {
		t.Fatalf("CountOpenAuthoredByOrAssignedTo(alice): %v", err)
	}
	if got != 2 {
		t.Errorf("alice: want 2 (own opens, dedup), got %d", got)
	}

	got, err = s.CountOpenAuthoredByOrAssignedTo(ctx, bobID)
	if err != nil {
		t.Fatalf("CountOpenAuthoredByOrAssignedTo(bob): %v", err)
	}
	if got != 1 {
		t.Errorf("bob: want 1 (assignee only), got %d", got)
	}
}
