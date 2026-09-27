package service_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

var dirsTestAuthor = service.GitAuthor{Name: "Tester", Email: "tester@example.com"}

type repoDirsEnv struct {
	db    *sql.DB
	root  string
	repos *service.RepoService
	code  *service.CodeService
}

func newRepoDirsEnv(t *testing.T) repoDirsEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	root := t.TempDir()
	git := config.GitConfig{ReposRoot: root}
	return repoDirsEnv{
		db:    db,
		root:  root,
		repos: service.NewRepoService(store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db), nil, nil, git),
		code:  service.NewCodeService(git),
	}
}

func (e repoDirsEnv) seedUser(t *testing.T) (int64, string) {
	t.Helper()
	suffix := testutil.UniqueSuffix(t)
	return testutil.SeedUser(t, e.db, suffix), "testuser_" + suffix
}

func (e repoDirsEnv) dirs(owner, name string) (gitDir, wikiDir string) {
	base := filepath.Join(e.root, owner, name)
	return base + ".git", base + ".wiki.git"
}

func (e repoDirsEnv) createWithWiki(t *testing.T, owner, name string) int64 {
	t.Helper()
	repo, err := e.repos.Create(context.Background(), owner, name, "", false, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("create %s/%s: %v", owner, name, err)
	}
	if err := e.code.WikiPageSave(owner, name, "Home", "wiki of "+owner+"/"+name, dirsTestAuthor, ""); err != nil {
		t.Fatalf("save wiki page: %v", err)
	}
	return repo.ID
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestRepoService_DeleteAndRestoreMoveTheWiki(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	repoID := env.createWithWiki(t, owner, "withwiki")
	gitDir, wikiDir := env.dirs(owner, "withwiki")

	if err := env.repos.Delete(ctx, repoID, ownerID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	for _, dir := range []string{gitDir, wikiDir} {
		if pathExists(dir) {
			t.Errorf("%s is still at its live path after delete", filepath.Base(dir))
		}
		if matches, _ := filepath.Glob(dir + ".deleted.*"); len(matches) != 1 {
			t.Errorf("%s: want one soft-deleted copy, got %v", filepath.Base(dir), matches)
		}
	}

	if err := env.repos.Restore(ctx, repoID, ownerID, false); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if !pathExists(gitDir) {
		t.Error("git dir not restored")
	}
	if _, found, _ := env.code.WikiPageGet(owner, "withwiki", "Home"); !found {
		t.Error("wiki page lost across delete and restore")
	}
}

func TestRepoService_PurgeExpiredRemovesTheWiki(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	repoID := env.createWithWiki(t, owner, "purged")
	gitDir, wikiDir := env.dirs(owner, "purged")

	if err := env.repos.Delete(ctx, repoID, ownerID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	testutil.Exec(t, env.db, `UPDATE repositories SET deleted_at = NOW() - INTERVAL '31 days' WHERE id = $1`, repoID)
	if err := env.repos.PurgeExpired(ctx); err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}

	for _, dir := range []string{gitDir, wikiDir} {
		if pathExists(dir) {
			t.Errorf("%s survived the purge at its live path", filepath.Base(dir))
		}
		if matches, _ := filepath.Glob(dir + ".deleted.*"); len(matches) != 0 {
			t.Errorf("%s: soft-deleted copies survived the purge: %v", filepath.Base(dir), matches)
		}
	}
}

func TestRepoService_TransferMovesTheWiki(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	fromID, from := env.seedUser(t)
	_, to := env.seedUser(t)
	env.createWithWiki(t, from, "moving")

	repo, err := env.repos.Get(ctx, from, "moving")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := env.repos.TransferRepo(ctx, repo, fromID, to); err != nil {
		t.Fatalf("TransferRepo: %v", err)
	}

	if _, oldWiki := env.dirs(from, "moving"); pathExists(oldWiki) {
		t.Error("wiki left behind at the old owner's path")
	}
	if _, found, _ := env.code.WikiPageGet(to, "moving", "Home"); !found {
		t.Error("wiki page did not move with the repo")
	}
}
