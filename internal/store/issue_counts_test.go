package store_test

// Integration test for the folded IssueStore.CountsForUser;
// skipped when TEST_DATABASE_DSN is unset.

import (
	"context"
	"fmt"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

func TestIssueStore_CountsForUser(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	aliceID, bobID, cleanup := seedTwoUsers(t, ctx, "isscnt")
	defer cleanup()

	suffix := fmt.Sprintf("%d_%s", os.Getpid(), t.Name())
	repoID := insertRepo(t, ctx, db, aliceID, "alice", "icr_"+suffix)

	mkIssue := func(number int, authorID int64, state string) int64 {
		var id int64
		if err := db.QueryRowContext(ctx,
			`INSERT INTO issues (repo_id, number, author_id, title, body, state)
			 VALUES ($1, $2, $3, $4, '', $5) RETURNING id`,
			repoID, number, authorID, fmt.Sprintf("i%d", number), state,
		).Scan(&id); err != nil {
			t.Fatalf("insert issue %d: %v", number, err)
		}
		return id
	}
	assign := func(issueID int64) {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO issue_assignees (issue_id, user_id) VALUES ($1, $2)`,
			issueID, aliceID,
		); err != nil {
			t.Fatalf("assign issue %d: %v", issueID, err)
		}
	}
	comments := store.NewCommentStore(db)
	mentions := store.NewMentionStore(db)
	mention := func(issueID int64) {
		c := &model.Comment{RepoID: repoID, IssueID: &issueID, AuthorID: bobID, Body: "@alice"}
		if err := comments.Create(ctx, c); err != nil {
			t.Fatalf("create comment on issue %d: %v", issueID, err)
		}
		if err := mentions.Create(ctx, c.ID, aliceID); err != nil {
			t.Fatalf("create mention on issue %d: %v", issueID, err)
		}
	}

	mkIssue(1, aliceID, "open")          // created:open
	mkIssue(2, aliceID, "closed")        // created:closed
	assign(mkIssue(3, bobID, "open"))    // assigned:open
	assign(mkIssue(4, bobID, "closed"))  // assigned:closed
	mention(mkIssue(5, bobID, "open"))   // mentioned:open
	mention(mkIssue(6, bobID, "closed")) // mentioned:closed

	got, err := store.NewIssueStore(db).CountsForUser(ctx, aliceID)
	if err != nil {
		t.Fatalf("CountsForUser: %v", err)
	}
	want := map[string]int{
		"created:open": 1, "created:closed": 1,
		"assigned:open": 1, "assigned:closed": 1,
		"mentioned:open": 1, "mentioned:closed": 1,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("CountsForUser[%q] = %d, want %d", k, got[k], v)
		}
	}
}
