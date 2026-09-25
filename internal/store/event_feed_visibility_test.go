package store_test

// Integration test guarding the activity feed against leaking private-repo
// events to users without read access; skipped when TEST_DATABASE_DSN is unset.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// A user assigned to an issue is "involved" in it, so its events reach their
// feed — but only when they can actually read the hosting repository.
func TestEventStore_FeedExcludesUnreadablePrivateRepos(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	aliceID, bobID, cleanup := seedTwoUsers(t, ctx, "feedvis")
	defer cleanup()

	suffix := fmt.Sprintf("%d_%s", os.Getpid(), t.Name())
	mkRepo := func(name string, private bool) int64 {
		var id int64
		if err := db.QueryRowContext(ctx,
			`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
			 VALUES ($1, 'bob', $2, '', $3, 'main') RETURNING id`,
			bobID, name, private,
		).Scan(&id); err != nil {
			t.Fatalf("insert repo %s: %v", name, err)
		}
		return id
	}
	privateNoAccess := mkRepo("priv_"+suffix, true)
	privateWithPerm := mkRepo("privperm_"+suffix, true)
	public := mkRepo("pub_"+suffix, false)

	if _, err := db.ExecContext(ctx,
		`INSERT INTO permissions (repo_id, user_id, role) VALUES ($1, $2, 'reader')`,
		privateWithPerm, aliceID,
	); err != nil {
		t.Fatalf("grant permission: %v", err)
	}

	es := store.NewEventStore(db)
	issueNo := 0
	// Records, in repoID, an issue assigned to alice plus a comment event on it.
	mkInvolvementEvent := func(repoID int64, repoName string) {
		issueNo++
		var issueID int64
		if err := db.QueryRowContext(ctx,
			`INSERT INTO issues (repo_id, number, author_id, title, body, state)
			 VALUES ($1, $2, $3, 'i', '', 'open') RETURNING id`,
			repoID, issueNo, bobID,
		).Scan(&issueID); err != nil {
			t.Fatalf("insert issue: %v", err)
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO issue_assignees (issue_id, user_id) VALUES ($1, $2)`,
			issueID, aliceID,
		); err != nil {
			t.Fatalf("assign issue: %v", err)
		}
		payload, err := json.Marshal(map[string]any{"number": issueNo, "kind": "issue", "body": "secret comment"})
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		rid := repoID
		if err := es.Record(ctx, &model.Event{
			ActorID: bobID, ActorName: "bob", RepoID: &rid, RepoName: repoName, OwnerName: "bob",
			EventType: model.EventComment, Payload: payload,
		}); err != nil {
			t.Fatalf("record event: %v", err)
		}
	}
	mkInvolvementEvent(privateNoAccess, "priv")
	mkInvolvementEvent(privateWithPerm, "privperm")
	mkInvolvementEvent(public, "pub")

	events, err := es.ListForFeed(ctx, aliceID, 1, 50)
	if err != nil {
		t.Fatalf("ListForFeed: %v", err)
	}
	seen := map[int64]bool{}
	for _, e := range events {
		if e.RepoID != nil {
			seen[*e.RepoID] = true
		}
	}
	if seen[privateNoAccess] {
		t.Error("ListForFeed leaked an event from a private repo alice cannot read")
	}
	if !seen[public] {
		t.Error("ListForFeed dropped the public-repo involvement event")
	}
	if !seen[privateWithPerm] {
		t.Error("ListForFeed dropped a private-repo event alice has permission to read")
	}

	counts, err := es.FeedCounts(ctx, aliceID)
	if err != nil {
		t.Fatalf("FeedCounts: %v", err)
	}
	if counts["all"] != 2 {
		t.Errorf(`FeedCounts["all"] = %d, want 2 (public + permitted private; unreadable private excluded)`, counts["all"])
	}
}
