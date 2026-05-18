package store_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

func TestPullReviewStore_ListPullIDsAwaitingReviewer(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	aliceID, bobID, cleanup := seedTwoUsers(t, ctx, "reviewreq")
	defer cleanup()

	suffix := fmt.Sprintf("%d", os.Getpid())

	// alice owns the repo; bob is requested as reviewer.
	var repoID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
		 VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
		aliceID, "alice_"+suffix, "reviewreqrepo_"+suffix,
	).Scan(&repoID); err != nil {
		t.Fatalf("insert repo: %v", err)
	}

	var pullID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO pull_requests (repo_id, number, author_id, title, state, head_branch, base_branch)
		 VALUES ($1, 1, $2, 't1', 'open', 'h1', 'main') RETURNING id`,
		repoID, aliceID,
	).Scan(&pullID); err != nil {
		t.Fatalf("insert pull: %v", err)
	}

	s := store.NewPullReviewStore(db)

	if err := s.RequestReview(ctx, pullID, repoID, bobID, "bob_"+suffix); err != nil {
		t.Fatalf("RequestReview: %v", err)
	}

	got, err := s.ListPullIDsAwaitingReviewer(ctx, bobID)
	if err != nil {
		t.Fatalf("ListPullIDsAwaitingReviewer(bob): %v", err)
	}
	if len(got) != 1 || got[0] != pullID {
		t.Errorf("bob: want [%d], got %v", pullID, got)
	}

	// alice was never requested as a reviewer.
	got, err = s.ListPullIDsAwaitingReviewer(ctx, aliceID)
	if err != nil {
		t.Fatalf("ListPullIDsAwaitingReviewer(alice): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("alice: want [], got %v", got)
	}
}
