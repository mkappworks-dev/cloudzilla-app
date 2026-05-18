package store_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

func TestIssueStore_ListForUser(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	aliceID, _, cleanup := seedTwoUsers(t, ctx, "issuelist")
	defer cleanup()

	suffix := fmt.Sprintf("%d", os.Getpid())

	var repoID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
		 VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
		aliceID, "alice_"+suffix, "issuelistrepo_"+suffix,
	).Scan(&repoID); err != nil {
		t.Fatalf("insert repo: %v", err)
	}

	var openIssue int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO issues (repo_id, number, author_id, title, body, state)
		 VALUES ($1, 1, $2, 't1', '', 'open') RETURNING id`,
		repoID, aliceID,
	).Scan(&openIssue); err != nil {
		t.Fatalf("insert open issue: %v", err)
	}

	s := store.NewIssueStore(db)

	got, err := s.ListForUser(ctx, aliceID, "created", "open")
	if err != nil {
		t.Fatalf("ListForUser(created, open): %v", err)
	}
	if len(got) != 1 || got[0].ID != openIssue {
		t.Errorf("ListForUser(created, open): want [%d], got %+v", openIssue, got)
	}

	got, err = s.ListForUser(ctx, aliceID, "created", "closed")
	if err != nil {
		t.Fatalf("ListForUser(created, closed): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListForUser(created, closed): want empty, got %+v", got)
	}
}
