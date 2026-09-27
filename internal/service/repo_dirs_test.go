package service_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

var dirsTestAuthor = service.GitAuthor{Name: "Tester", Email: "tester@example.com"}

type repoDirsEnv struct {
	db    *sql.DB
	root  string
	repos *service.RepoService
	orgs  *service.OrgService
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
		orgs:  service.NewOrgService(store.NewOrgStore(db), store.NewRepoStore(db), store.NewUserStore(db), git),
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

func (e repoDirsEnv) createOrg(t *testing.T, ownerID int64) *model.Organization {
	t.Helper()
	org, err := e.orgs.Create(context.Background(), ownerID, "testorg_"+testutil.UniqueSuffix(t), "", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	t.Cleanup(func() { testutil.Exec(t, e.db, `DELETE FROM organizations WHERE id = $1`, org.ID) })
	return org
}

// dropRow deletes a repo row and leaves its directories behind, as account
// deletion used to.
func (e repoDirsEnv) dropRow(t *testing.T, owner, name string) {
	t.Helper()
	testutil.Exec(t, e.db, `DELETE FROM repositories WHERE owner_name = $1 AND name = $2`, owner, name)
}

func (e repoDirsEnv) rowCount(t *testing.T, owner, name string) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM repositories WHERE owner_name = $1 AND name = $2`, owner, name).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return n
}

func headOf(t *testing.T, gitDir string) string {
	t.Helper()
	repo, err := gogit.PlainOpen(gitDir)
	if err != nil {
		t.Fatalf("open %s: %v", gitDir, err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("head of %s: %v", gitDir, err)
	}
	return head.Hash().String()
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

func TestRepoService_Create_RefusesLeftoverDirs(t *testing.T) {
	for _, leftover := range []string{"git and wiki", "git only", "wiki only"} {
		t.Run(leftover, func(t *testing.T) {
			env := newRepoDirsEnv(t)
			ctx := context.Background()
			_, owner := env.seedUser(t)
			env.createWithWiki(t, owner, "left")
			env.dropRow(t, owner, "left")
			gitDir, wikiDir := env.dirs(owner, "left")
			switch leftover {
			case "git only":
				_ = os.RemoveAll(wikiDir)
			case "wiki only":
				_ = os.RemoveAll(gitDir)
			}
			var head string
			if pathExists(gitDir) {
				head = headOf(t, gitDir)
			}

			_, err := env.repos.Create(ctx, owner, "left", "", false, service.RepoInitOptions{})
			if !errors.Is(err, service.ErrRepoNameTaken) {
				t.Errorf("Create over a leftover dir: want ErrRepoNameTaken, got %v", err)
			}
			if n := env.rowCount(t, owner, "left"); n != 0 {
				t.Errorf("Create left %d rows behind", n)
			}
			if head != "" && headOf(t, gitDir) != head {
				t.Error("leftover repo was modified")
			}
			if leftover != "git only" {
				if _, found, _ := env.code.WikiPageGet(owner, "left", "Home"); !found {
					t.Error("leftover wiki was modified")
				}
			}
		})
	}
}

func TestRepoService_Create_RemovesItsDirWhenTheRowIsRefused(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	repoID := env.createWithWiki(t, owner, "again")
	if err := env.repos.Delete(ctx, repoID, ownerID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// The soft-deleted row still holds (owner_id, name).
	_, err := env.repos.Create(ctx, owner, "again", "", false, service.RepoInitOptions{})
	if !errors.Is(err, service.ErrRepoNameTaken) {
		t.Errorf("want ErrRepoNameTaken, got %v", err)
	}
	if gitDir, _ := env.dirs(owner, "again"); pathExists(gitDir) {
		t.Error("claimed dir left behind after the insert failed")
	}
}

func TestOrgService_CreateRepo_RefusesLeftoverDir(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	ownerID, _ := env.seedUser(t)
	org := env.createOrg(t, ownerID)
	if _, err := env.orgs.CreateRepo(ctx, org.ID, ownerID, "left", "", true, service.RepoInitOptions{AddREADME: true}); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	env.dropRow(t, org.Name, "left")
	gitDir, _ := env.dirs(org.Name, "left")
	head := headOf(t, gitDir)

	_, err := env.orgs.CreateRepo(ctx, org.ID, ownerID, "left", "", false, service.RepoInitOptions{})
	if !errors.Is(err, service.ErrRepoNameTaken) {
		t.Errorf("want ErrRepoNameTaken, got %v", err)
	}
	if n := env.rowCount(t, org.Name, "left"); n != 0 {
		t.Errorf("CreateRepo left %d rows behind", n)
	}
	if headOf(t, gitDir) != head {
		t.Error("leftover repo was modified")
	}
}

// repositories is unique on (owner_id, name), which does not stop a second
// org owner from reusing a name.
func TestOrgService_CreateRepo_SecondOwnerCannotReuseAName(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	firstID, _ := env.seedUser(t)
	secondID, _ := env.seedUser(t)
	org := env.createOrg(t, firstID)
	if err := env.orgs.AddMember(ctx, org.ID, firstID, secondID, model.OrgRoleOwner); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if _, err := env.orgs.CreateRepo(ctx, org.ID, firstID, "shared", "", true, service.RepoInitOptions{}); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	_, err := env.orgs.CreateRepo(ctx, org.ID, secondID, "shared", "", false, service.RepoInitOptions{})
	if !errors.Is(err, service.ErrRepoNameTaken) {
		t.Errorf("want ErrRepoNameTaken, got %v", err)
	}
	if n := env.rowCount(t, org.Name, "shared"); n != 1 {
		t.Errorf("want 1 row for %s/shared, got %d", org.Name, n)
	}
}

func TestRepoService_CreateFromTemplate_RefusesLeftoverDirAndPathNames(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	tmplOwnerID, tmplOwner := env.seedUser(t)
	userID, user := env.seedUser(t)
	tmpl, err := env.repos.Create(ctx, tmplOwner, "tmpl", "", false, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	if err := env.repos.SetTemplate(ctx, tmpl.ID, tmplOwnerID, true); err != nil {
		t.Fatalf("SetTemplate: %v", err)
	}

	env.createWithWiki(t, user, "fromtmpl")
	env.dropRow(t, user, "fromtmpl")
	gitDir, _ := env.dirs(user, "fromtmpl")
	head := headOf(t, gitDir)
	_, err = env.repos.CreateFromTemplate(ctx, tmpl.ID, userID, user, "fromtmpl", "")
	if !errors.Is(err, service.ErrRepoNameTaken) {
		t.Errorf("leftover dir: want ErrRepoNameTaken, got %v", err)
	}
	if n := env.rowCount(t, user, "fromtmpl"); n != 0 {
		t.Errorf("leftover dir: %d rows left behind", n)
	}
	if headOf(t, gitDir) != head {
		t.Error("template was copied into the leftover repo")
	}

	if _, err := env.repos.CreateFromTemplate(ctx, tmpl.ID, userID, user, "../escape", ""); err == nil {
		t.Error("a path in the name was accepted")
	}
	if n := env.rowCount(t, user, "../escape"); n != 0 {
		testutil.Exec(t, env.db, `DELETE FROM repositories WHERE owner_name = $1 AND name = $2`, user, "../escape")
		t.Errorf("a path in the name was stored")
	}
	if pathExists(filepath.Join(env.root, "escape.git")) {
		t.Error("template copied outside the owner's directory")
	}
}

func TestRepoService_Fork_SkipsLeftoverDir(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	_, owner := env.seedUser(t)
	userID, user := env.seedUser(t)
	orig, err := env.repos.Create(ctx, owner, "forkme", "", false, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("create original: %v", err)
	}
	env.createWithWiki(t, user, "forkme")
	env.dropRow(t, user, "forkme")
	leftoverDir, _ := env.dirs(user, "forkme")
	leftoverHead := headOf(t, leftoverDir)

	forked, err := env.repos.Fork(ctx, owner, orig.Name, userID, user)
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	if forked.Name != "forkme-1" {
		t.Errorf("fork took the leftover's name %q", forked.Name)
	}
	if headOf(t, leftoverDir) != leftoverHead {
		t.Error("fork was copied into the leftover repo")
	}
	origDir, _ := env.dirs(owner, "forkme")
	if forkDir, _ := env.dirs(user, forked.Name); pathExists(forkDir) && headOf(t, forkDir) != headOf(t, origDir) {
		t.Error("fork does not match the original")
	}
}

func TestRepoService_Transfer_RefusesLeftoverWiki(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	fromID, from := env.seedUser(t)
	_, to := env.seedUser(t)
	repo, err := env.repos.Create(ctx, from, "moved", "", true, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	env.createWithWiki(t, to, "moved")
	env.dropRow(t, to, "moved")
	toGitDir, _ := env.dirs(to, "moved")
	_ = os.RemoveAll(toGitDir)

	if err := env.repos.TransferRepo(ctx, repo, fromID, to); !errors.Is(err, service.ErrRepoNameTaken) {
		t.Errorf("want ErrRepoNameTaken, got %v", err)
	}
	if got, err := env.repos.Get(ctx, from, "moved"); err != nil || got.OwnerID != fromID {
		t.Errorf("repo row moved despite the refusal: %+v, %v", got, err)
	}
	if fromGitDir, _ := env.dirs(from, "moved"); !pathExists(fromGitDir) {
		t.Error("repo dir moved despite the refusal")
	}
}
