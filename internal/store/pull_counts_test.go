package store_test

// Integration test for the folded PullStore.CountsForUser;
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

func TestPullStore_CountsForUser(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	aliceID, bobID, cleanup := seedTwoUsers(t, ctx, "pullcnt")
	defer cleanup()

	suffix := fmt.Sprintf("%d_%s", os.Getpid(), t.Name())
	repoID := insertRepo(t, ctx, db, aliceID, "alice", "pcr_"+suffix)

	mkPull := func(number int, authorID int64, state string) int64 {
		var id int64
		if err := db.QueryRowContext(ctx,
			`INSERT INTO pull_requests (repo_id, number, author_id, title, state, head_branch, base_branch)
			 VALUES ($1, $2, $3, $4, $5, $6, 'main') RETURNING id`,
			repoID, number, authorID, fmt.Sprintf("p%d", number), state, fmt.Sprintf("h%d", number),
		).Scan(&id); err != nil {
			t.Fatalf("insert pull %d: %v", number, err)
		}
		return id
	}
	assign := func(pullID int64) {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO pull_assignees (pull_id, user_id) VALUES ($1, $2)`,
			pullID, aliceID,
		); err != nil {
			t.Fatalf("assign pull %d: %v", pullID, err)
		}
	}
	requestReview := func(pullID int64) {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO pull_reviews (pull_id, repo_id, author_id, state) VALUES ($1, $2, $3, 'pending')`,
			pullID, repoID, aliceID,
		); err != nil {
			t.Fatalf("request review on pull %d: %v", pullID, err)
		}
	}
	comments := store.NewCommentStore(db)
	mentions := store.NewMentionStore(db)
	mention := func(pullID int64) {
		c := &model.Comment{RepoID: repoID, PullID: &pullID, AuthorID: bobID, Body: "@alice"}
		if err := comments.Create(ctx, c); err != nil {
			t.Fatalf("create comment on pull %d: %v", pullID, err)
		}
		if err := mentions.Create(ctx, c.ID, aliceID); err != nil {
			t.Fatalf("create mention on pull %d: %v", pullID, err)
		}
	}

	mkPull(1, aliceID, "open")                // created:open
	mkPull(2, aliceID, "closed")              // created:closed
	assign(mkPull(3, bobID, "open"))          // assigned:open
	assign(mkPull(4, bobID, "closed"))        // assigned:closed
	requestReview(mkPull(5, bobID, "open"))   // review_requested:open
	requestReview(mkPull(6, bobID, "closed")) // review_requested:closed
	mention(mkPull(7, bobID, "open"))         // mentioned:open
	mention(mkPull(8, bobID, "closed"))       // mentioned:closed

	got, err := store.NewPullStore(db).CountsForUser(ctx, aliceID)
	if err != nil {
		t.Fatalf("CountsForUser: %v", err)
	}
	want := map[string]int{
		"created:open": 1, "created:closed": 1,
		"assigned:open": 1, "assigned:closed": 1,
		"review_requested:open": 1, "review_requested:closed": 1,
		"mentioned:open": 1, "mentioned:closed": 1,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("CountsForUser[%q] = %d, want %d", k, got[k], v)
		}
	}
}
