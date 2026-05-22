package store_test

// Integration test for the folded activity-feed scope counts;
// skipped when TEST_DATABASE_DSN is unset.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

func insertRepo(t *testing.T, ctx context.Context, db *sql.DB, ownerID int64, ownerName, name string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
		 VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
		ownerID, ownerName, name,
	).Scan(&id); err != nil {
		t.Fatalf("insert repo %s: %v", name, err)
	}
	return id
}

func TestEventStore_FeedCounts(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	aliceID, bobID, cleanup := seedTwoUsers(t, ctx, "evtfeed")
	defer cleanup()

	suffix := fmt.Sprintf("%d_%s", os.Getpid(), t.Name())
	aliceRepo := insertRepo(t, ctx, db, aliceID, "alice", "ar_"+suffix)  // alice owns
	bobRepo := insertRepo(t, ctx, db, bobID, "bob", "br_"+suffix)        // alice unrelated
	watchedRepo := insertRepo(t, ctx, db, bobID, "bob", "wr_"+suffix)    // alice watches

	if _, err := db.ExecContext(ctx,
		`INSERT INTO watches (user_id, repo_id, level) VALUES ($1, $2, 'watching')`,
		aliceID, watchedRepo,
	); err != nil {
		t.Fatalf("insert watch: %v", err)
	}

	es := store.NewEventStore(db)
	// 3 events alice performed (no repo) — counted only under "yours".
	for i := 0; i < 3; i++ {
		if err := es.Record(ctx, &model.Event{ActorID: aliceID, ActorName: "alice", EventType: model.EventPush}); err != nil {
			t.Fatalf("record alice event: %v", err)
		}
	}
	// Event on a repo alice owns — under "all" (not "watching").
	if err := es.Record(ctx, &model.Event{ActorID: bobID, ActorName: "bob", RepoID: &aliceRepo, EventType: model.EventPush}); err != nil {
		t.Fatalf("record alice-repo event: %v", err)
	}
	// Event on a repo alice watches — under "all" and "watching".
	if err := es.Record(ctx, &model.Event{ActorID: bobID, ActorName: "bob", RepoID: &watchedRepo, EventType: model.EventPush}); err != nil {
		t.Fatalf("record watched-repo event: %v", err)
	}
	// Event on an unrelated repo — under none of alice's scopes.
	if err := es.Record(ctx, &model.Event{ActorID: bobID, ActorName: "bob", RepoID: &bobRepo, EventType: model.EventPush}); err != nil {
		t.Fatalf("record bob-repo event: %v", err)
	}

	got, err := es.FeedCounts(ctx, aliceID)
	if err != nil {
		t.Fatalf("FeedCounts: %v", err)
	}
	for scope, want := range map[string]int{"all": 2, "yours": 3, "watching": 1} {
		if got[scope] != want {
			t.Errorf("FeedCounts[%q] = %d, want %d", scope, got[scope], want)
		}
	}
}
