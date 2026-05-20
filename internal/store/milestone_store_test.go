package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// TestMilestoneStore_SetIssue_CrossRepoRejected verifies that an attempt to
// attach an issue in repo A to a milestone in repo B is rejected with
// ErrMilestoneRepoMismatch — the cross-repo IDOR guard added in this branch.
func TestMilestoneStore_SetIssue_CrossRepoRejected(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)

	ownerID := testutil.SeedUser(t, db, suffix)
	repoA := testutil.SeedRepo(t, db, ownerID, "owner_"+suffix, suffix+"_a")
	repoB := testutil.SeedRepo(t, db, ownerID, "owner_"+suffix, suffix+"_b")

	s := store.NewMilestoneStore(db)
	issueAID := seedIssue(t, db, repoA, ownerID)
	milestoneBID := seedMilestone(t, db, repoB)

	err := s.SetIssue(ctx, issueAID, &milestoneBID)
	if !errors.Is(err, store.ErrMilestoneRepoMismatch) {
		t.Fatalf("SetIssue cross-repo: want ErrMilestoneRepoMismatch, got %v", err)
	}

	// Verify the issue's milestone_id was not modified.
	var current sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT milestone_id FROM issues WHERE id=$1`, issueAID).Scan(&current); err != nil {
		t.Fatalf("verify milestone_id: %v", err)
	}
	if current.Valid {
		t.Errorf("issue.milestone_id was set despite cross-repo rejection: %d", current.Int64)
	}
}

// TestMilestoneStore_SetIssue_NonexistentMilestone verifies that the new
// classifyMilestoneSetFailure helper returns ErrMilestoneNotFound (not
// ErrMilestoneRepoMismatch) when the milestone id does not exist at all.
func TestMilestoneStore_SetIssue_NonexistentMilestone(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)

	ownerID := testutil.SeedUser(t, db, suffix)
	repo := testutil.SeedRepo(t, db, ownerID, "owner_"+suffix, suffix)

	s := store.NewMilestoneStore(db)
	issueID := seedIssue(t, db, repo, ownerID)

	bogus := int64(-1)
	err := s.SetIssue(ctx, issueID, &bogus)
	if !errors.Is(err, store.ErrMilestoneNotFound) {
		t.Fatalf("SetIssue nonexistent: want ErrMilestoneNotFound, got %v", err)
	}
}

// TestMilestoneStore_SetIssue_SameRepoSucceeds is the positive-path control:
// the cross-repo guard must not interfere with legitimate same-repo attaches.
func TestMilestoneStore_SetIssue_SameRepoSucceeds(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)

	ownerID := testutil.SeedUser(t, db, suffix)
	repo := testutil.SeedRepo(t, db, ownerID, "owner_"+suffix, suffix)

	s := store.NewMilestoneStore(db)
	issueID := seedIssue(t, db, repo, ownerID)
	milestoneID := seedMilestone(t, db, repo)

	if err := s.SetIssue(ctx, issueID, &milestoneID); err != nil {
		t.Fatalf("SetIssue same-repo: %v", err)
	}

	var current sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT milestone_id FROM issues WHERE id=$1`, issueID).Scan(&current); err != nil {
		t.Fatalf("verify milestone_id: %v", err)
	}
	if !current.Valid || current.Int64 != milestoneID {
		t.Errorf("issue.milestone_id = %v, want %d", current, milestoneID)
	}
}

func seedIssue(t *testing.T, db *sql.DB, repoID, authorID int64) int64 {
	t.Helper()
	var id int64
	err := db.QueryRowContext(context.Background(),
		`INSERT INTO issues (repo_id, number, author_id, title, body, state, visibility)
		 VALUES ($1,
		         (SELECT COALESCE(MAX(number), 0) + 1 FROM issues WHERE repo_id = $1),
		         $2, 'test', '', 'open', 'public')
		 RETURNING id`,
		repoID, authorID,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seedIssue: %v", err)
	}
	t.Cleanup(func() {
		db.ExecContext(context.Background(), `DELETE FROM issues WHERE id = $1`, id)
	})
	return id
}

func seedMilestone(t *testing.T, db *sql.DB, repoID int64) int64 {
	t.Helper()
	var id int64
	err := db.QueryRowContext(context.Background(),
		`INSERT INTO milestones (repo_id, number, title, description, state)
		 VALUES ($1,
		         (SELECT COALESCE(MAX(number), 0) + 1 FROM milestones WHERE repo_id = $1),
		         'test', '', 'open')
		 RETURNING id`,
		repoID,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seedMilestone: %v", err)
	}
	t.Cleanup(func() {
		db.ExecContext(context.Background(), `DELETE FROM milestones WHERE id = $1`, id)
	})
	return id
}
