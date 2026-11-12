package service_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

func newAliasEnv(t *testing.T) (repoDirsEnv, *store.RepoAliasStore) {
	t.Helper()
	env := newRepoDirsEnv(t)
	aliases := store.NewRepoAliasStore(env.db)
	env.repos.WithAliasStore(aliases)
	return env, aliases
}

func wantNoAlias(t *testing.T, env repoDirsEnv, owner, name string) {
	t.Helper()
	if _, err := env.repos.ResolveAlias(context.Background(), owner, name); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("ResolveAlias(%s/%s) err = %v, want sql.ErrNoRows", owner, name, err)
	}
}

func TestRepoService_Create_OntoAliasNameSucceedsAndDropsIt(t *testing.T) {
	env, aliases := newAliasEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	moved := env.createWithWiki(t, owner, "moved")
	if err := aliases.Add(ctx, moved, owner, "Old"); err != nil {
		t.Fatal(err)
	}
	if got, err := env.repos.ResolveAlias(ctx, owner, "old"); err != nil || got.ID != moved {
		t.Fatalf("ResolveAlias = %v, %v; want repo %d", got, err, moved)
	}

	fresh, err := env.repos.Create(ctx, ownerID, owner, "old", "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatalf("create onto an alias name: %v", err)
	}

	wantNoAlias(t, env, owner, "old")
	if got, err := env.repos.Get(ctx, owner, "old"); err != nil || got.ID != fresh.ID {
		t.Errorf("Get = %v, %v; want the new repo %d", got, err, fresh.ID)
	}
}

func TestRepoService_AliasIsNotAHeldName(t *testing.T) {
	env, aliases := newAliasEnv(t)
	ctx := context.Background()
	_, owner := env.seedUser(t)
	moved := env.createWithWiki(t, owner, "moved")
	if err := aliases.Add(ctx, moved, owner, "old"); err != nil {
		t.Fatal(err)
	}

	if err := env.repos.CheckImportName(ctx, owner, "old"); err != nil {
		t.Errorf("CheckImportName on an alias name: %v, want nil", err)
	}
}

func TestRepoService_CreateFromImport_OntoAliasNameSucceedsAndDropsIt(t *testing.T) {
	env, aliases := newAliasEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	moved := env.createWithWiki(t, owner, "moved")
	if err := aliases.Add(ctx, moved, owner, "old"); err != nil {
		t.Fatal(err)
	}

	target := service.RepoTarget{ActorID: ownerID, OwnerName: owner, OwnerID: ownerID}
	if _, err := env.repos.CreateFromImport(ctx, target, "old", "", false, "trunk", seedImportedClone(t)); err != nil {
		t.Fatalf("import onto an alias name: %v", err)
	}
	wantNoAlias(t, env, owner, "old")
}

func TestRepoService_Fork_OntoAliasNameSucceedsAndDropsIt(t *testing.T) {
	env, aliases := newAliasEnv(t)
	ctx := context.Background()
	_, owner := env.seedUser(t)
	forkerID, forker := env.seedUser(t)
	env.createWithWiki(t, owner, "upstream")
	mine := env.createWithWiki(t, forker, "mine")
	if err := aliases.Add(ctx, mine, forker, "upstream"); err != nil {
		t.Fatal(err)
	}

	fork, err := env.repos.Fork(ctx, owner, "upstream", forkerID, forker, service.ForkOptions{})
	if err != nil {
		t.Fatalf("fork onto an alias name: %v", err)
	}
	if fork.Name != "upstream" {
		t.Errorf("fork named %q, want the alias name to count as free", fork.Name)
	}
	wantNoAlias(t, env, forker, "upstream")
}

func TestRepoService_Restore_AliasFollowsTheRepo(t *testing.T) {
	env, aliases := newAliasEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	repoID := env.createWithWiki(t, owner, "moved")
	if err := aliases.Add(ctx, repoID, owner, "old"); err != nil {
		t.Fatal(err)
	}

	if err := env.repos.Delete(ctx, repoID, ownerID); err != nil {
		t.Fatal(err)
	}
	wantNoAlias(t, env, owner, "old")

	if err := env.repos.Restore(ctx, repoID, ownerID, false); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got, err := env.repos.ResolveAlias(ctx, owner, "old"); err != nil || got.ID != repoID {
		t.Errorf("after restore ResolveAlias = %v, %v; want repo %d", got, err, repoID)
	}
}

func TestRepoService_Restore_DropsAliasHoldingItsName(t *testing.T) {
	env, aliases := newAliasEnv(t)
	ctx := context.Background()
	ownerID, _ := env.seedUser(t)
	org := env.createOrg(t, ownerID)
	binned, err := env.orgs.CreateRepo(ctx, org.ID, ownerID, "binned", "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	other, err := env.orgs.CreateRepo(ctx, org.ID, ownerID, "other", "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.repos.Delete(ctx, binned.ID, ownerID); err != nil {
		t.Fatal(err)
	}
	// A soft-deleted org repo does not hold its name, so a rename can leave an alias there.
	if err := aliases.Add(ctx, other.ID, org.Name, "binned"); err != nil {
		t.Fatal(err)
	}

	if err := env.repos.Restore(ctx, binned.ID, ownerID, false); err != nil {
		t.Fatalf("Restore must succeed and drop the alias on its name: %v", err)
	}

	wantNoAlias(t, env, org.Name, "binned")
	if got, err := env.repos.Get(ctx, org.Name, "binned"); err != nil || got.ID != binned.ID {
		t.Errorf("Get = %v, %v; want the restored repo %d", got, err, binned.ID)
	}
}

func TestRepoService_PurgeExpired_RemovesAliases(t *testing.T) {
	env, aliases := newAliasEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	repoID := env.createWithWiki(t, owner, "moved")
	if err := aliases.Add(ctx, repoID, owner, "old"); err != nil {
		t.Fatal(err)
	}
	if err := env.repos.Delete(ctx, repoID, ownerID); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-31 * 24 * time.Hour)
	if _, err := env.db.Exec(`UPDATE repositories SET deleted_at = $2 WHERE id = $1`, repoID, old); err != nil {
		t.Fatal(err)
	}

	if err := env.repos.PurgeExpired(ctx); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM repo_aliases WHERE repo_id = $1`, repoID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("purge left %d aliases", n)
	}
}
