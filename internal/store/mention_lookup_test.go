package store_test

// Integration tests for the MentionStore lookup helpers:
//   - MentionStore.ListIssueIDsMentioning
//   - MentionStore.ListPullIDsMentioning
//
// Skipped when TEST_DATABASE_DSN is unset, matching the project's
// existing integration-test pattern.

import (
	"context"
	"fmt"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

func TestMentionStore_Lookup(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	aliceID, _, cleanup := seedTwoUsers(t, ctx, "mention")
	defer cleanup()

	suffix := fmt.Sprintf("%d", os.Getpid())

	var repoID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
		 VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
		aliceID, "alice_"+suffix, "mentionrepo_"+suffix,
	).Scan(&repoID); err != nil {
		t.Fatalf("insert repo: %v", err)
	}

	var issueID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO issues (repo_id, number, author_id, title, body, state)
		 VALUES ($1, 1, $2, 't1', '', 'open') RETURNING id`,
		repoID, aliceID,
	).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}

	var pullID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO pull_requests (repo_id, number, author_id, title, state, head_branch, base_branch)
		 VALUES ($1, 1, $2, 't1', 'open', 'h1', 'main') RETURNING id`,
		repoID, aliceID,
	).Scan(&pullID); err != nil {
		t.Fatalf("insert pull: %v", err)
	}

	commentStore := store.NewCommentStore(db)
	mentionStore := store.NewMentionStore(db)

	issueComment := &model.Comment{
		RepoID:   repoID,
		IssueID:  &issueID,
		AuthorID: aliceID,
		Body:     "@alice on an issue",
	}
	if err := commentStore.Create(ctx, issueComment); err != nil {
		t.Fatalf("create issue comment: %v", err)
	}
	if err := mentionStore.Create(ctx, issueComment.ID, aliceID); err != nil {
		t.Fatalf("create issue mention: %v", err)
	}

	pullComment := &model.Comment{
		RepoID:   repoID,
		PullID:   &pullID,
		AuthorID: aliceID,
		Body:     "@alice on a pull",
	}
	if err := commentStore.Create(ctx, pullComment); err != nil {
		t.Fatalf("create pull comment: %v", err)
	}
	if err := mentionStore.Create(ctx, pullComment.ID, aliceID); err != nil {
		t.Fatalf("create pull mention: %v", err)
	}

	issueIDs, err := mentionStore.ListIssueIDsMentioning(ctx, aliceID)
	if err != nil {
		t.Fatalf("ListIssueIDsMentioning: %v", err)
	}
	if !containsID(issueIDs, issueID) {
		t.Errorf("ListIssueIDsMentioning: want %d in %v", issueID, issueIDs)
	}
	if containsID(issueIDs, pullID) && pullID != issueID {
		t.Errorf("ListIssueIDsMentioning: unexpectedly returned pull id %d", pullID)
	}

	pullIDs, err := mentionStore.ListPullIDsMentioning(ctx, aliceID)
	if err != nil {
		t.Fatalf("ListPullIDsMentioning: %v", err)
	}
	if !containsID(pullIDs, pullID) {
		t.Errorf("ListPullIDsMentioning: want %d in %v", pullID, pullIDs)
	}
}

func containsID(ids []int64, want int64) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
