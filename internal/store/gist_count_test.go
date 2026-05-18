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
