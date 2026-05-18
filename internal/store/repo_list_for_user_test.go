package store_test

// Integration test for RepoStore.ListForUser. Requires TEST_DATABASE_DSN and
// skips otherwise (via the shared openTestDB helper).

import (
	"context"
	"fmt"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

func repoIDsOf(repos []model.Repository) []int64 {
	ids := make([]int64, len(repos))
	for i, r := range repos {
		ids[i] = r.ID
	}
	return ids
}

func TestRepoStore_ListForUser(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	aliceID, bobID, cleanup := seedTwoUsers(t, ctx, "listforuser")
	defer cleanup()

	suffix := fmt.Sprintf("%d", os.Getpid())

	// Repo A: owned by alice.
	var repoA int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
		 VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
		aliceID, "alice_"+suffix, "repoA_"+suffix,
	).Scan(&repoA); err != nil {
		t.Fatalf("insert repo A: %v", err)
	}

	// Repo B: owned by bob, alice collaborates on it.
	var repoB int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
		 VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
		bobID, "bob_"+suffix, "repoB_"+suffix,
	).Scan(&repoB); err != nil {
		t.Fatalf("insert repo B: %v", err)
	}

	s := store.NewRepoStore(db)

	// alice collaborates on repo B via a non-owner permission row.
	if err := s.AddPermission(ctx, repoB, aliceID, "writer"); err != nil {
		t.Fatalf("add collaborator permission: %v", err)
	}

	owned, err := s.ListForUser(ctx, aliceID, "owned")
	if err != nil {
		t.Fatalf("ListForUser(owned): %v", err)
	}
	if len(owned) != 1 || owned[0].ID != repoA {
		t.Errorf("owned: want [repoA=%d], got %v", repoA, repoIDsOf(owned))
	}

	collab, err := s.ListForUser(ctx, aliceID, "collaborator")
	if err != nil {
		t.Fatalf("ListForUser(collaborator): %v", err)
	}
	if len(collab) != 1 || collab[0].ID != repoB {
		t.Errorf("collaborator: want [repoB=%d], got %v", repoB, repoIDsOf(collab))
	}

	all, err := s.ListForUser(ctx, aliceID, "all")
	if err != nil {
		t.Fatalf("ListForUser(all): %v", err)
	}
	if len(all) != 2 {
		t.Errorf("all: want 2 repos (A and B), got %v", repoIDsOf(all))
	}
	gotA, gotB := false, false
	for _, r := range all {
		if r.ID == repoA {
			gotA = true
		}
		if r.ID == repoB {
			gotB = true
		}
	}
	if !gotA || !gotB {
		t.Errorf("all: want repoA=%d and repoB=%d present, got %v", repoA, repoB, repoIDsOf(all))
	}
}
