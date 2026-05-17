package store_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

func TestPullStore_ListForUser(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	aliceID, _, cleanup := seedTwoUsers(t, ctx, "pulllist")
	defer cleanup()

	suffix := fmt.Sprintf("%d", os.Getpid())

	var repoID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
		 VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
		aliceID, "alice_"+suffix, "pulllistrepo_"+suffix,
	).Scan(&repoID); err != nil {
		t.Fatalf("insert repo: %v", err)
	}

	var openPR int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO pull_requests (repo_id, number, author_id, title, state, head_branch, base_branch)
		 VALUES ($1, 1, $2, 't1', 'open', 'h1', 'main') RETURNING id`,
		repoID, aliceID,
	).Scan(&openPR); err != nil {
		t.Fatalf("insert open pr: %v", err)
	}

	s := store.NewPullStore(db)

	got, err := s.ListForUser(ctx, aliceID, "created", "open")
	if err != nil {
		t.Fatalf("ListForUser(created, open): %v", err)
	}
	if len(got) != 1 || got[0].ID != openPR {
		t.Errorf("ListForUser(created, open): want [%d], got %+v", openPR, got)
	}

	got, err = s.ListForUser(ctx, aliceID, "created", "closed")
	if err != nil {
		t.Fatalf("ListForUser(created, closed): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListForUser(created, closed): want empty, got %+v", got)
	}
}
