package store_test

import (
	"context"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

func TestGistStore_ListWithCounts(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	aliceID, _, cleanup := seedTwoUsers(t, ctx, "lwc")
	defer cleanup()

	gs := store.NewGistStore(db)

	parent := &model.Gist{
		ID: "lwc_parent_" + t.Name(), OwnerID: aliceID, OwnerName: "alice",
		Description: "parent", Public: true,
	}
	if err := gs.Create(ctx, parent, []model.GistFile{{Filename: "a.go", Content: "x"}}); err != nil {
		t.Fatalf("create parent: %v", err)
	}

	fork := &model.Gist{
		ID: "lwc_fork_" + t.Name(), OwnerID: aliceID, OwnerName: "alice",
		Description: "fork", Public: true,
	}
	if err := gs.Create(ctx, fork, []model.GistFile{{Filename: "a.go", Content: "x"}}); err != nil {
		t.Fatalf("create fork: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE gists SET forked_from_id=$1 WHERE id=$2`, parent.ID, fork.ID); err != nil {
		t.Fatalf("set fork: %v", err)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO gist_stars (gist_id, user_id) VALUES ($1, $2)`, parent.ID, aliceID); err != nil {
		t.Fatalf("insert star: %v", err)
	}

	var aliceUsername string
	if err := db.QueryRowContext(ctx, `SELECT username FROM users WHERE id = $1`, aliceID).Scan(&aliceUsername); err != nil {
		t.Fatalf("lookup alice username: %v", err)
	}

	rows, err := gs.ListWithCounts(ctx, aliceUsername)
	if err != nil {
		t.Fatalf("ListWithCounts: %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows (parent + fork), got %d", len(rows))
	}

	var parentRow, forkRow *model.GistListRow
	for i := range rows {
		switch rows[i].ID {
		case parent.ID:
			parentRow = &rows[i]
		case fork.ID:
			forkRow = &rows[i]
		}
	}
	if parentRow == nil {
		t.Fatalf("parent gist %q not in ListWithCounts results", parent.ID)
	}
	if forkRow == nil {
		t.Fatalf("fork gist %q not in ListWithCounts results", fork.ID)
	}
	if parentRow.ForkCount < 1 {
		t.Errorf("parent ForkCount: want >=1, got %d", parentRow.ForkCount)
	}
	if forkRow.ForkCount != 0 {
		t.Errorf("fork ForkCount: want 0, got %d", forkRow.ForkCount)
	}
	if parentRow.StarCount < 1 || parentRow.FileCount < 1 {
		t.Errorf("parent counts: StarCount=%d FileCount=%d, both want >=1",
			parentRow.StarCount, parentRow.FileCount)
	}
}

func TestGistStore_ListPrivateByOwner(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	aliceID, _, cleanup := seedTwoUsers(t, ctx, "lpriv")
	defer cleanup()

	gs := store.NewGistStore(db)

	priv := &model.Gist{
		ID: "lpriv_priv_" + t.Name(), OwnerID: aliceID, OwnerName: "alice",
		Description: "private gist", Public: false,
	}
	if err := gs.Create(ctx, priv, []model.GistFile{{Filename: "b.go", Content: "y"}}); err != nil {
		t.Fatalf("create private gist: %v", err)
	}

	pub := &model.Gist{
		ID: "lpriv_pub_" + t.Name(), OwnerID: aliceID, OwnerName: "alice",
		Description: "public gist", Public: true,
	}
	if err := gs.Create(ctx, pub, []model.GistFile{{Filename: "c.go", Content: "z"}}); err != nil {
		t.Fatalf("create public gist: %v", err)
	}

	rows, err := gs.ListPrivateByOwner(ctx, aliceID, 1, 20)
	if err != nil {
		t.Fatalf("ListPrivateByOwner: %v", err)
	}

	var foundPriv, foundPub bool
	for _, r := range rows {
		if r.ID == priv.ID {
			foundPriv = true
		}
		if r.ID == pub.ID {
			foundPub = true
		}
	}
	if !foundPriv {
		t.Errorf("private gist not found in ListPrivateByOwner results")
	}
	if foundPub {
		t.Errorf("public gist should not appear in ListPrivateByOwner results")
	}
}
