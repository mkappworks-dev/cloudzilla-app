package store_test

// Integration tests for RepoAliasStore and the alias rules in RepoStore.

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func aliasCount(t *testing.T, db *sql.DB, ownerName, name string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM repo_aliases WHERE owner_name = $1 AND name = $2`, ownerName, name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRepoAliasStore_Resolve_IsCaseInsensitiveAndReturnsTheRepo(t *testing.T) {
	db := openStoreDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerName := "alias_owner_" + suffix
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	aliases := store.NewRepoAliasStore(db)
	if err := aliases.Add(ctx, repoID, ownerName, "OldName_"+suffix); err != nil {
		t.Fatal(err)
	}

	got, err := aliases.Resolve(ctx, "ALIAS_OWNER_"+suffix, "oldname_"+suffix)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.ID != repoID || got.Name != "testrepo_"+suffix {
		t.Errorf("resolved %d/%s, want the repo %d under its current name", got.ID, got.Name, repoID)
	}
	if _, err := aliases.Resolve(ctx, ownerName, "never_"+suffix); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("unknown path: err = %v, want sql.ErrNoRows", err)
	}
}

func TestRepoAliasStore_Resolve_IgnoresSoftDeletedRepos(t *testing.T) {
	db := openStoreDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerName := "alias_owner_" + suffix
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	aliases := store.NewRepoAliasStore(db)
	repos := store.NewRepoStore(db)
	if err := aliases.Add(ctx, repoID, ownerName, "old_"+suffix); err != nil {
		t.Fatal(err)
	}

	if err := repos.Delete(ctx, repoID, ownerName, ownerID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := aliases.Resolve(ctx, ownerName, "old_"+suffix); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("soft-deleted repo: err = %v, want sql.ErrNoRows", err)
	}
	if n := aliasCount(t, db, ownerName, "old_"+suffix); n != 1 {
		t.Errorf("soft delete changed the alias row: %d rows, want 1", n)
	}

	if err := repos.Restore(ctx, repoID); err != nil {
		t.Fatal(err)
	}
	if got, err := aliases.Resolve(ctx, ownerName, "old_"+suffix); err != nil || got.ID != repoID {
		t.Errorf("restored repo: %v, %v; want it to resolve again", got, err)
	}
}

func TestRepoAliasStore_Add_NewerAliasReplacesAndDropRemoves(t *testing.T) {
	db := openStoreDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerName := "alias_owner_" + suffix
	ownerID := testutil.SeedUser(t, db, suffix)
	first := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	var second int64
	if err := db.QueryRow(`INSERT INTO repositories (owner_id, owner_name, name) VALUES ($1, $2, 'second') RETURNING id`, ownerID, ownerName).Scan(&second); err != nil {
		t.Fatal(err)
	}
	aliases := store.NewRepoAliasStore(db)

	if err := aliases.Add(ctx, first, ownerName, "shared_"+suffix); err != nil {
		t.Fatal(err)
	}
	if err := aliases.Add(ctx, second, ownerName, "SHARED_"+suffix); err != nil {
		t.Fatalf("a newer alias at the same path must replace the older: %v", err)
	}
	got, err := aliases.Resolve(ctx, ownerName, "shared_"+suffix)
	if err != nil || got.ID != second {
		t.Fatalf("Resolve = %v, %v; want repo %d", got, err, second)
	}

	if err := aliases.Drop(ctx, ownerName, "Shared_"+suffix); err != nil {
		t.Fatal(err)
	}
	if err := aliases.Drop(ctx, ownerName, "Shared_"+suffix); err != nil {
		t.Errorf("dropping a missing alias must not fail: %v", err)
	}
	if _, err := aliases.Resolve(ctx, ownerName, "shared_"+suffix); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("after Drop: err = %v, want sql.ErrNoRows", err)
	}
}

func TestRepoAliasStore_DropByRepo_RemovesOnlyThatReposAliases(t *testing.T) {
	db := openStoreDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerName := "alias_owner_" + suffix
	ownerID := testutil.SeedUser(t, db, suffix)
	mine := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	var other int64
	if err := db.QueryRow(`INSERT INTO repositories (owner_id, owner_name, name) VALUES ($1, $2, 'other') RETURNING id`, ownerID, ownerName).Scan(&other); err != nil {
		t.Fatal(err)
	}
	aliases := store.NewRepoAliasStore(db)
	for _, a := range []struct {
		repo int64
		name string
	}{{mine, "a1"}, {mine, "a2"}, {other, "b1"}} {
		if err := aliases.Add(ctx, a.repo, ownerName, a.name+"_"+suffix); err != nil {
			t.Fatal(err)
		}
	}

	if err := aliases.DropByRepo(ctx, mine); err != nil {
		t.Fatal(err)
	}
	if aliasCount(t, db, ownerName, "a1_"+suffix)+aliasCount(t, db, ownerName, "a2_"+suffix) != 0 {
		t.Error("DropByRepo left the repo's aliases")
	}
	if aliasCount(t, db, ownerName, "b1_"+suffix) != 1 {
		t.Error("DropByRepo removed another repo's alias")
	}
}

func TestRepoStore_Create_DropsAliasAtItsPath(t *testing.T) {
	db := openStoreDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerName := "alias_owner_" + suffix
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	aliases := store.NewRepoAliasStore(db)
	repos := store.NewRepoStore(db)
	if err := aliases.Add(ctx, repoID, ownerName, "Reused_"+suffix); err != nil {
		t.Fatal(err)
	}

	fresh := &model.Repository{OwnerID: ownerID, OwnerName: ownerName, Name: "reused_" + suffix, DefaultBranch: "main"}
	if err := repos.CreateWithOwnerName(ctx, fresh); err != nil {
		t.Fatalf("create onto an alias path: %v", err)
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM repositories WHERE id = $1`, fresh.ID) })

	if _, err := aliases.Resolve(ctx, ownerName, "reused_"+suffix); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("alias survived the create: err = %v", err)
	}

	forked := &model.Repository{OwnerID: ownerID, OwnerName: ownerName, Name: "forked_" + suffix, DefaultBranch: "main", ForkOfID: &repoID}
	if err := aliases.Add(ctx, repoID, ownerName, "forked_"+suffix); err != nil {
		t.Fatal(err)
	}
	if err := repos.Fork(ctx, forked); err != nil {
		t.Fatalf("fork onto an alias path: %v", err)
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM repositories WHERE id = $1`, forked.ID) })
	if n := aliasCount(t, db, ownerName, "forked_"+suffix); n != 0 {
		t.Errorf("alias survived the fork: %d rows", n)
	}
}

func TestRepoStore_Restore_DropsAliasesTheRestoredPathOutranks(t *testing.T) {
	db := openStoreDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerName := "alias_owner_" + suffix
	ownerID := testutil.SeedUser(t, db, suffix)
	trashed := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	var holder, live int64
	if err := db.QueryRow(`INSERT INTO repositories (owner_id, owner_name, name) VALUES ($1, $2, 'holder') RETURNING id`, ownerID, ownerName).Scan(&holder); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO repositories (owner_id, owner_name, name) VALUES ($1, $2, $3) RETURNING id`, ownerID, ownerName, "taken_"+suffix).Scan(&live); err != nil {
		t.Fatal(err)
	}
	aliases := store.NewRepoAliasStore(db)
	repos := store.NewRepoStore(db)
	// Newer alias at the trashed repo's own path, and one of its own aliases a live repo now holds.
	if err := aliases.Add(ctx, holder, ownerName, "testrepo_"+suffix); err != nil {
		t.Fatal(err)
	}
	if err := aliases.Add(ctx, trashed, ownerName, "taken_"+suffix); err != nil {
		t.Fatal(err)
	}
	if err := aliases.Add(ctx, trashed, ownerName, "kept_"+suffix); err != nil {
		t.Fatal(err)
	}
	if err := repos.Delete(ctx, trashed, ownerName, ownerID, time.Now()); err != nil {
		t.Fatal(err)
	}

	if err := repos.Restore(ctx, trashed); err != nil {
		t.Fatalf("Restore must succeed despite conflicting aliases: %v", err)
	}

	if n := aliasCount(t, db, ownerName, "testrepo_"+suffix); n != 0 {
		t.Error("alias at the restored path survived")
	}
	if n := aliasCount(t, db, ownerName, "taken_"+suffix); n != 0 {
		t.Error("alias shadowed by a live repo survived")
	}
	if got, err := aliases.Resolve(ctx, ownerName, "kept_"+suffix); err != nil || got.ID != trashed {
		t.Errorf("unrelated alias of the restored repo: %v, %v; want it kept", got, err)
	}
}

func TestRepoStore_PurgeExpired_CascadesToAliases(t *testing.T) {
	db := openStoreDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerName := "alias_owner_" + suffix
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	aliases := store.NewRepoAliasStore(db)
	repos := store.NewRepoStore(db)
	if err := aliases.Add(ctx, repoID, ownerName, "gone_"+suffix); err != nil {
		t.Fatal(err)
	}
	if err := repos.Delete(ctx, repoID, ownerName, ownerID, time.Now().Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}

	purged, err := repos.PurgeExpired(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range purged {
		found = found || r.ID == repoID
	}
	if !found {
		t.Fatal("repo was not purged")
	}
	if n := aliasCount(t, db, ownerName, "gone_"+suffix); n != 0 {
		t.Errorf("purge left %d aliases, want the cascade to remove them", n)
	}
}
