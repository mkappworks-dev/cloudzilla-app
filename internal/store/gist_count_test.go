package store_test

import (
	"context"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

func TestGistStore_CountByOwner(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	aliceID, _, cleanup := seedTwoUsers(t, ctx, "gistcnt")
	defer cleanup()

	gs := store.NewGistStore(db)

	got, err := gs.CountByOwner(ctx, aliceID)
	if err != nil {
		t.Fatalf("CountByOwner(alice) before insert: %v", err)
	}
	if got != 0 {
		t.Errorf("CountByOwner(alice): want 0, got %d", got)
	}

	g := &model.Gist{
		ID:          "gistcnt_" + t.Name(),
		OwnerID:     aliceID,
		OwnerName:   "alice",
		Description: "test gist",
		Public:      true,
	}
	files := []model.GistFile{{Filename: "a.txt", Content: "hello"}}
	if err := gs.Create(ctx, g, files); err != nil {
		t.Fatalf("Create gist: %v", err)
	}

	got, err = gs.CountByOwner(ctx, aliceID)
	if err != nil {
		t.Fatalf("CountByOwner(alice) after insert: %v", err)
	}
	if got != 1 {
		t.Errorf("CountByOwner(alice): want 1, got %d", got)
	}
}

func mustCreateGist(t *testing.T, ctx context.Context, gs *store.GistStore, id string, ownerID int64, ownerName string, public bool) {
	t.Helper()
	g := &model.Gist{ID: id, OwnerID: ownerID, OwnerName: ownerName, Description: "test gist", Public: public}
	files := []model.GistFile{{Filename: "a.txt", Content: "hello"}}
	if err := gs.Create(ctx, g, files); err != nil {
		t.Fatalf("create gist %s: %v", id, err)
	}
}

func TestGistStore_CountPublic(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	aliceID, _, cleanup := seedTwoUsers(t, ctx, "gistpub")
	defer cleanup()

	gs := store.NewGistStore(db)

	// Instance-wide count; assert the delta so concurrent tests don't interfere.
	before, err := gs.CountPublic(ctx)
	if err != nil {
		t.Fatalf("CountPublic before: %v", err)
	}

	mustCreateGist(t, ctx, gs, "gistpub_pub_"+t.Name(), aliceID, "alice", true)
	mustCreateGist(t, ctx, gs, "gistpub_priv_"+t.Name(), aliceID, "alice", false)

	after, err := gs.CountPublic(ctx)
	if err != nil {
		t.Fatalf("CountPublic after: %v", err)
	}
	if after-before != 1 {
		t.Errorf("CountPublic delta: want 1 (private excluded), got %d", after-before)
	}
}

func TestGistStore_CountPrivateByOwner(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	aliceID, bobID, cleanup := seedTwoUsers(t, ctx, "gistpriv")
	defer cleanup()

	gs := store.NewGistStore(db)

	got, err := gs.CountPrivateByOwner(ctx, aliceID)
	if err != nil {
		t.Fatalf("CountPrivateByOwner(alice) before: %v", err)
	}
	if got != 0 {
		t.Errorf("CountPrivateByOwner(alice): want 0, got %d", got)
	}

	mustCreateGist(t, ctx, gs, "gistpriv_a_priv_"+t.Name(), aliceID, "alice", false)
	mustCreateGist(t, ctx, gs, "gistpriv_a_pub_"+t.Name(), aliceID, "alice", true)
	mustCreateGist(t, ctx, gs, "gistpriv_b_priv_"+t.Name(), bobID, "bob", false)

	got, err = gs.CountPrivateByOwner(ctx, aliceID)
	if err != nil {
		t.Fatalf("CountPrivateByOwner(alice) after: %v", err)
	}
	if got != 1 {
		t.Errorf("CountPrivateByOwner(alice): want 1 (own private only), got %d", got)
	}
}
