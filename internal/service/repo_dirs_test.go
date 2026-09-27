package service_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

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

// backdate moves owner/name's only soft-deleted copy, and its row's
// deleted_at, d into the past.
func (e repoDirsEnv) backdate(t *testing.T, repoID int64, owner, name string, d time.Duration) {
	t.Helper()
	gitDir, wikiDir := e.dirs(owner, name)
	matches, _ := filepath.Glob(gitDir + ".deleted.*")
	if len(matches) != 1 {
		t.Fatalf("want one soft-deleted copy of %s/%s, got %v", owner, name, matches)
	}
	suffix := strings.TrimPrefix(matches[0], gitDir)
	sec, err := strconv.ParseInt(strings.TrimPrefix(suffix, ".deleted."), 10, 64)
	if err != nil {
		t.Fatalf("parse %s: %v", suffix, err)
	}
	at := time.Unix(sec, 0).Add(-d)
	for _, dir := range []string{gitDir, wikiDir} {
		if err := os.Rename(dir+suffix, dir+".deleted."+strconv.FormatInt(at.Unix(), 10)); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("backdate %s: %v", filepath.Base(dir), err)
		}
	}
	testutil.Exec(t, e.db, `UPDATE repositories SET deleted_at = $1 WHERE id = $2`, at, repoID)
}

type sameNameCopies struct {
	org                   *model.Organization
	firstID, secondID     int64
	first, second         int64
	firstHead, secondHead string
}

// twoDeletedCopies leaves two org owners' soft-deleted repos named "x" side by
// side, the first deleted age earlier. repositories is unique per owner_id,
// so only the copies' suffixes tell them apart.
func (e repoDirsEnv) twoDeletedCopies(t *testing.T, age time.Duration) sameNameCopies {
	t.Helper()
	ctx := context.Background()
	var c sameNameCopies
	c.firstID, _ = e.seedUser(t)
	c.secondID, _ = e.seedUser(t)
	c.org = e.createOrg(t, c.firstID)
	if err := e.orgs.AddMember(ctx, c.org.ID, c.firstID, c.secondID, model.OrgRoleOwner); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	gitDir, _ := e.dirs(c.org.Name, "x")
	for i, ownerID := range []int64{c.firstID, c.secondID} {
		repo, err := e.orgs.CreateRepo(ctx, c.org.ID, ownerID, "x", fmt.Sprintf("copy %d", i), true, service.RepoInitOptions{AddREADME: true})
		if err != nil {
			t.Fatalf("CreateRepo copy %d: %v", i, err)
		}
		if err := e.code.WikiPageSave(c.org.Name, "x", "Home", fmt.Sprintf("wiki of copy %d", i), dirsTestAuthor, ""); err != nil {
			t.Fatalf("save wiki of copy %d: %v", i, err)
		}
		head := headOf(t, gitDir)
		if err := e.repos.Delete(ctx, repo.ID, ownerID); err != nil {
			t.Fatalf("delete copy %d: %v", i, err)
		}
		if i == 0 {
			e.backdate(t, repo.ID, c.org.Name, "x", age)
			c.first, c.firstHead = repo.ID, head
		} else {
			c.second, c.secondHead = repo.ID, head
		}
	}
	return c
}

func (e repoDirsEnv) wantLive(t *testing.T, owner, name, head, wiki string) {
	t.Helper()
	gitDir, _ := e.dirs(owner, name)
	if !pathExists(gitDir) {
		t.Fatalf("%s/%s has no repo dir", owner, name)
	}
	if got := headOf(t, gitDir); got != head {
		t.Errorf("%s/%s holds %s, want %s", owner, name, got, head)
	}
	if got, _, _ := e.code.WikiPageGet(owner, name, "Home"); got != wiki {
		t.Errorf("%s/%s wiki reads %q, want %q", owner, name, got, wiki)
	}
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

func TestRepoService_RestoreTakesTheRowsOwnCopy(t *testing.T) {
	env := newRepoDirsEnv(t)
	c := env.twoDeletedCopies(t, time.Hour)

	if err := env.repos.Restore(context.Background(), c.first, c.firstID, false); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	env.wantLive(t, c.org.Name, "x", c.firstHead, "wiki of copy 0")
}

// Both rows would resolve to the new holder's directory.
func TestRepoService_Restore_RefusesANameWithANewHolder(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	firstID, _ := env.seedUser(t)
	secondID, _ := env.seedUser(t)
	org := env.createOrg(t, firstID)
	if err := env.orgs.AddMember(ctx, org.ID, firstID, secondID, model.OrgRoleOwner); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	first, err := env.orgs.CreateRepo(ctx, org.ID, firstID, "x", "", true, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	if err := env.repos.Delete(ctx, first.ID, firstID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	gitDir, wikiDir := env.dirs(org.Name, "x")
	copies, _ := filepath.Glob(gitDir + ".deleted.*")
	wikiCopies, _ := filepath.Glob(wikiDir + ".deleted.*")
	for _, c := range append(copies, wikiCopies...) {
		if err := os.RemoveAll(c); err != nil {
			t.Fatalf("remove copy: %v", err)
		}
	}
	if _, err := env.orgs.CreateRepo(ctx, org.ID, secondID, "x", "", true, service.RepoInitOptions{AddREADME: true}); err != nil {
		t.Fatalf("CreateRepo by the new holder: %v", err)
	}
	head := headOf(t, gitDir)

	if err := env.repos.Restore(ctx, first.ID, firstID, false); !errors.Is(err, service.ErrRepoNameTaken) {
		t.Errorf("want ErrRepoNameTaken, got %v", err)
	}
	var live int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM repositories WHERE owner_name = $1 AND name = 'x' AND deleted_at IS NULL`, org.Name).Scan(&live); err != nil {
		t.Fatalf("count live rows: %v", err)
	}
	if live != 1 {
		t.Errorf("want 1 live row for %s/x, got %d", org.Name, live)
	}
	if headOf(t, gitDir) != head {
		t.Error("new holder's repo was modified")
	}
}

func TestRepoService_Restore_KeepsAWikiLeftAtTheLivePath(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	ownerID, owner := env.seedUser(t)
	repoID := env.createWithWiki(t, owner, "old")
	if err := env.repos.Delete(ctx, repoID, ownerID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, wikiDir := env.dirs(owner, "old")
	copies, _ := filepath.Glob(wikiDir + ".deleted.*")
	if len(copies) != 1 {
		t.Fatalf("want one wiki copy, got %v", copies)
	}
	// Deletes before wikis moved with their repo left the wiki here.
	if err := os.Rename(copies[0], wikiDir); err != nil {
		t.Fatalf("put wiki back: %v", err)
	}

	if err := env.repos.Restore(ctx, repoID, ownerID, false); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got, _, _ := env.code.WikiPageGet(owner, "old", "Home"); got != "wiki of "+owner+"/old" {
		t.Errorf("wiki reads %q after restore", got)
	}
}

func TestRepoService_PurgeExpiredLeavesOtherCopiesOfTheName(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	c := env.twoDeletedCopies(t, 31*24*time.Hour)

	if err := env.repos.PurgeExpired(ctx); err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	gitDir, _ := env.dirs(c.org.Name, "x")
	if matches, _ := filepath.Glob(gitDir + ".deleted.*"); len(matches) != 1 {
		t.Errorf("want only the unexpired copy left, got %v", matches)
	}
	if err := env.repos.Restore(ctx, c.second, c.secondID, false); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	env.wantLive(t, c.org.Name, "x", c.secondHead, "wiki of copy 1")
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
	env.backdate(t, repoID, owner, "purged", 31*24*time.Hour)
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
