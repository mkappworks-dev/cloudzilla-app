package service_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func (e repoDirsEnv) users() *service.UserService {
	return service.NewUserService(store.NewUserStore(e.db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"}).
		WithRepoService(e.repos)
}

func (e repoDirsEnv) userExists(t *testing.T, id int64) bool {
	t.Helper()
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM users WHERE id = $1`, id).Scan(&n); err != nil {
		t.Fatalf("count users: %v", err)
	}
	return n == 1
}

func TestUserService_DeleteUser_RemovesRepoDirs(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	userID, user := env.seedUser(t)
	env.createWithWiki(t, user, "live")
	gone := env.createWithWiki(t, user, "gone")
	if err := env.repos.Delete(ctx, gone, userID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	if err := env.users().DeleteUser(ctx, userID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	if env.userExists(t, userID) {
		t.Fatal("user row survived")
	}
	entries, err := os.ReadDir(filepath.Join(env.root, user))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read owner dir: %v", err)
	}
	for _, e := range entries {
		t.Errorf("%s/%s survived the account", user, e.Name())
	}
}

func TestUserService_DeleteUser_RemovesAStrandedWiki(t *testing.T) {
	env := newRepoDirsEnv(t)
	userID, user := env.seedUser(t)
	repoID := env.createWithWiki(t, user, "old")
	env.strandWiki(t, repoID, user, "old", time.Hour)

	if err := env.users().DeleteUser(context.Background(), userID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, wikiDir := env.dirs(user, "old"); pathExists(wikiDir) {
		t.Error("stranded wiki survived the account")
	}
}

func TestUserService_DeleteUser_FailedDeleteKeepsRepos(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	userID, user := env.seedUser(t)
	_, other := env.seedUser(t)
	env.createWithWiki(t, user, "kept")
	gitDir, wikiDir := env.dirs(user, "kept")
	head := headOf(t, gitDir)
	othersRepo, err := env.repos.Create(ctx, other, "theirs", "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatalf("create other's repo: %v", err)
	}
	// issues.author_id has no ON DELETE action, so this issue blocks the user delete.
	testutil.Exec(t, env.db, `INSERT INTO issues (repo_id, number, author_id, title) VALUES ($1, 1, $2, 'blocks delete')`, othersRepo.ID, userID)

	if err := env.users().DeleteUser(ctx, userID); err == nil {
		t.Fatal("DeleteUser succeeded despite the authored issue")
	}

	if !env.userExists(t, userID) {
		t.Fatal("user row gone after a failed delete")
	}
	if !pathExists(gitDir) || headOf(t, gitDir) != head {
		t.Error("repo not back at its path after the failed delete")
	}
	if _, found, _ := env.code.WikiPageGet(user, "kept", "Home"); !found {
		t.Errorf("wiki not back at %s after the failed delete", wikiDir)
	}
	if matches, _ := filepath.Glob(filepath.Join(env.root, user, "*.deleted.*")); len(matches) != 0 {
		t.Errorf("moved-aside copies left after the failed delete: %v", matches)
	}
}

func TestUserService_DeleteUser_RefusesWhileOwningOrgRepos(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	userID, _ := env.seedUser(t)
	org := env.createOrg(t, userID)
	repo, err := env.orgs.CreateRepo(ctx, org.ID, userID, "orgowned", "", true, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	if err := env.users().DeleteUser(ctx, userID); !errors.Is(err, service.ErrOwnsOrgRepos) {
		t.Errorf("want ErrOwnsOrgRepos, got %v", err)
	}
	if !env.userExists(t, userID) {
		t.Fatal("user deleted while owning an org repo")
	}
	if _, err := env.repos.Get(ctx, org.Name, "orgowned"); err != nil {
		t.Errorf("org repo row gone: %v", err)
	}
	if gitDir, _ := env.dirs(org.Name, "orgowned"); !pathExists(gitDir) {
		t.Error("org repo dir moved")
	}

	if err := env.repos.Delete(ctx, repo.ID, userID); err != nil {
		t.Fatalf("soft delete org repo: %v", err)
	}
	if err := env.users().DeleteUser(ctx, userID); err != nil {
		t.Errorf("DeleteUser after deleting the org repo: %v", err)
	}
}

// With the row cascaded away nothing can restore or purge the copy, and
// another owner's copy of the same name must survive.
func TestUserService_DeleteUser_RemovesItsDeletedOrgRepoCopies(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	c := env.twoDeletedCopies(t, time.Hour)

	if err := env.users().DeleteUser(ctx, c.firstID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	gitDir, _ := env.dirs(c.org.Name, "x")
	if matches, _ := filepath.Glob(gitDir + ".deleted.*"); len(matches) != 1 {
		t.Errorf("want only the other owner's copy left, got %v", matches)
	}
	if err := env.repos.Restore(ctx, c.second, c.secondID, false); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	env.wantLive(t, c.org.Name, "x", c.secondHead, "wiki of copy 1")
}
