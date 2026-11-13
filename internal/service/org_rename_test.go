package service_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type orgRenameEnv struct {
	repoDirsEnv
	ownerID int64
	org     *model.Organization
	repoID  int64
}

func newOrgRenameEnv(t *testing.T) orgRenameEnv {
	t.Helper()
	env, _ := newAliasEnv(t)
	env.repos.WithMirrorStore(store.NewMirrorStore(env.db))
	env.orgs.WithRepoService(env.repos)
	ownerID, _ := env.seedUser(t)
	org := env.createOrg(t, ownerID)
	repo, err := env.orgs.CreateRepo(context.Background(), org.ID, ownerID, "core", "", false, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("create org repo: %v", err)
	}
	if err := env.code.WikiPageSave(org.Name, "core", "Home", "wiki", dirsTestAuthor, ""); err != nil {
		t.Fatalf("save wiki page: %v", err)
	}
	return orgRenameEnv{repoDirsEnv: env, ownerID: ownerID, org: org, repoID: repo.ID}
}

func newOwnerName(t *testing.T) string { return "renamed_" + testutil.UniqueSuffix(t) }

func TestOrgService_Rename_MovesDirsAndRowsAndAliasesTheOldName(t *testing.T) {
	env := newOrgRenameEnv(t)
	ctx := context.Background()
	oldName, newName := env.org.Name, newOwnerName(t)

	if err := env.orgs.RenameOrg(ctx, env.org, env.ownerID, newName); err != nil {
		t.Fatalf("RenameOrg: %v", err)
	}

	oldGit, oldWiki := env.dirs(oldName, "core")
	newGit, newWiki := env.dirs(newName, "core")
	if pathExists(oldGit) || pathExists(oldWiki) {
		t.Error("old dirs are still on disk")
	}
	if !pathExists(newGit) || !pathExists(newWiki) {
		t.Error("new dirs are missing")
	}
	if got, err := env.orgs.Get(ctx, newName); err != nil || got.ID != env.org.ID {
		t.Fatalf("Get(new) = %v, %v", got, err)
	}
	if _, err := env.orgs.Get(ctx, oldName); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Get(old) err = %v, want sql.ErrNoRows", err)
	}
	repo, err := env.repos.Get(ctx, newName, "core")
	if err != nil || repo.ID != env.repoID || repo.OwnerName != newName {
		t.Fatalf("Get(new/core) = %v, %v", repo, err)
	}
	if moved, err := env.repos.ResolveAlias(ctx, oldName, "core"); err != nil || moved.ID != env.repoID {
		t.Errorf("ResolveAlias(old/core) = %v, %v; want repo %d", moved, err, env.repoID)
	}
	if moved, err := env.orgs.ResolveAlias(ctx, strings.ToUpper(oldName)); err != nil || moved.ID != env.org.ID {
		t.Errorf("ResolveAlias(%s) = %v, %v; want org %d", strings.ToUpper(oldName), moved, err, env.org.ID)
	}
}

func TestOrgService_Rename_ChainStaysFlatAndRenamingBackDropsTheAlias(t *testing.T) {
	env := newOrgRenameEnv(t)
	ctx := context.Background()
	first, second, third := env.org.Name, newOwnerName(t), "third_"+testutil.UniqueSuffix(t)

	cur := env.org
	for _, next := range []string{second, third, first} {
		if err := env.orgs.RenameOrg(ctx, cur, env.ownerID, next); err != nil {
			t.Fatalf("RenameOrg to %s: %v", next, err)
		}
		var err error
		if cur, err = env.orgs.Get(ctx, next); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := env.orgs.ResolveAlias(ctx, first); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("ResolveAlias(%s), the current name, err = %v, want sql.ErrNoRows", first, err)
	}
	for _, name := range []string{second, third} {
		if got, err := env.orgs.ResolveAlias(ctx, name); err != nil || got.ID != env.org.ID {
			t.Errorf("ResolveAlias(%s) = %v, %v; want org %d", name, got, err, env.org.ID)
		}
	}
	if repo, err := env.repos.ResolveAlias(ctx, third, "core"); err != nil || repo.OwnerName != first {
		t.Errorf("ResolveAlias(%s/core) = %v, %v; want a repo of %s", third, repo, err, first)
	}
}

func TestOrgService_Rename_CaseOnlyChangeKeepsNoAlias(t *testing.T) {
	env := newOrgRenameEnv(t)
	ctx := context.Background()
	upper := strings.ToUpper(env.org.Name)

	if err := env.orgs.RenameOrg(ctx, env.org, env.ownerID, upper); err != nil {
		t.Fatalf("RenameOrg: %v", err)
	}
	if _, err := env.orgs.ResolveAlias(ctx, env.org.Name); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("ResolveAlias err = %v, want sql.ErrNoRows", err)
	}
	if gitDir, _ := env.dirs(upper, "core"); !pathExists(gitDir) {
		t.Error("repo is not under the new spelling")
	}
}

func TestOrgService_Rename_ClaimingTheOldNameDeletesTheAlias(t *testing.T) {
	env := newOrgRenameEnv(t)
	ctx := context.Background()
	oldName := env.org.Name
	if err := env.orgs.RenameOrg(ctx, env.org, env.ownerID, newOwnerName(t)); err != nil {
		t.Fatal(err)
	}

	claimed, err := env.orgs.Create(ctx, env.ownerID, oldName, "", "")
	if err != nil {
		t.Fatalf("create org with the old name: %v", err)
	}
	testutil.DeleteOrgOnCleanup(t, env.db, claimed.ID)

	if _, err := env.orgs.ResolveAlias(ctx, oldName); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("org alias survived the claim: %v", err)
	}
	wantNoAlias(t, env.repoDirsEnv, oldName, "core")
}

func TestOrgService_Rename_ANewUserWithTheOldNameDeletesTheAlias(t *testing.T) {
	env := newOrgRenameEnv(t)
	ctx := context.Background()
	oldName := env.org.Name
	if err := env.orgs.RenameOrg(ctx, env.org, env.ownerID, newOwnerName(t)); err != nil {
		t.Fatal(err)
	}

	testutil.Exec(t, env.db,
		`INSERT INTO users (username, email, password_hash, is_superadmin) VALUES ($1, $2, 'x', false)`,
		oldName, oldName+"@test.invalid")
	t.Cleanup(func() { testutil.Exec(t, env.db, `DELETE FROM users WHERE username = $1`, oldName) })

	if _, err := env.orgs.ResolveAlias(ctx, oldName); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("org alias survived the claim: %v", err)
	}
	wantNoAlias(t, env.repoDirsEnv, oldName, "core")
}

func TestOrgService_Rename_RefusesAUsersNameInAnyCase(t *testing.T) {
	env := newOrgRenameEnv(t)
	_, user := env.seedUser(t)

	for _, name := range []string{user, strings.ToUpper(user)} {
		if err := env.orgs.RenameOrg(context.Background(), env.org, env.ownerID, name); !errors.Is(err, service.ErrOrgNameTaken) {
			t.Errorf("RenameOrg to %q err = %v, want ErrOrgNameTaken", name, err)
		}
	}
	if gitDir, _ := env.dirs(env.org.Name, "core"); !pathExists(gitDir) {
		t.Error("a refused rename moved the repo")
	}
}

func TestOrgService_Rename_RefusesAnotherOrgsNameAndInvalidNames(t *testing.T) {
	env := newOrgRenameEnv(t)
	ctx := context.Background()
	other := env.createOrg(t, env.ownerID)

	if err := env.orgs.RenameOrg(ctx, env.org, env.ownerID, other.Name); !errors.Is(err, service.ErrOrgNameTaken) {
		t.Errorf("rename onto another org: err = %v, want ErrOrgNameTaken", err)
	}
	for _, name := range []string{"", "has space", "admin", strings.Repeat("a", 40)} {
		if err := env.orgs.RenameOrg(ctx, env.org, env.ownerID, name); !errors.Is(err, service.ErrInvalidOrgName) {
			t.Errorf("rename to %q: err = %v, want ErrInvalidOrgName", name, err)
		}
	}
	if err := env.orgs.RenameOrg(ctx, env.org, env.ownerID, env.org.Name); !errors.Is(err, service.ErrOrgNameUnchanged) {
		t.Errorf("rename to the same name: err = %v, want ErrOrgNameUnchanged", err)
	}
}

func TestOrgService_Rename_RefusesANonOwner(t *testing.T) {
	env := newOrgRenameEnv(t)
	ctx := context.Background()
	memberID, _ := env.seedUser(t)
	if err := env.orgs.AddMember(ctx, env.org.ID, env.ownerID, memberID, model.OrgRoleMember); err != nil {
		t.Fatal(err)
	}
	outsiderID, _ := env.seedUser(t)

	for _, id := range []int64{memberID, outsiderID} {
		if err := env.orgs.RenameOrg(ctx, env.org, id, newOwnerName(t)); !errors.Is(err, service.ErrForbidden) {
			t.Errorf("RenameOrg as user %d: err = %v, want ErrForbidden", id, err)
		}
	}
	if got, _ := env.orgs.Get(ctx, env.org.Name); got == nil {
		t.Error("a refused rename changed the name")
	}
}

func TestOrgService_Rename_AFailedRowUpdateMovesTheDirBack(t *testing.T) {
	env := newOrgRenameEnv(t)
	ctx := context.Background()
	stale := *env.org
	stale.ID = 0 // no such row: the dir moves, the row lock finds nothing

	if err := env.orgs.RenameOrg(ctx, &stale, env.ownerID, newOwnerName(t)); err == nil {
		t.Fatal("RenameOrg succeeded for a missing row")
	}
	if gitDir, wikiDir := env.dirs(env.org.Name, "core"); !pathExists(gitDir) || !pathExists(wikiDir) {
		t.Error("dirs were not moved back")
	}
}

func TestOrgService_Rename_RefusesADirLeftBehindAtTheNewName(t *testing.T) {
	env := newOrgRenameEnv(t)
	newName := newOwnerName(t)
	leftover, _ := env.dirs(newName, "stray")
	if err := os.MkdirAll(leftover, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := env.orgs.RenameOrg(context.Background(), env.org, env.ownerID, newName); !errors.Is(err, service.ErrOrgNameTaken) {
		t.Errorf("err = %v, want ErrOrgNameTaken", err)
	}
}

func TestOrgService_Rename_UpdatesEventsAndNotificationsText(t *testing.T) {
	env := newOrgRenameEnv(t)
	ctx := context.Background()
	oldName, newName := env.org.Name, newOwnerName(t)
	testutil.Exec(t, env.db,
		`INSERT INTO events (actor_id, repo_id, repo_name, owner_name, event_type) VALUES ($1, $2, 'core', $3, 'push')`,
		env.ownerID, env.repoID, oldName)
	testutil.Exec(t, env.db,
		`INSERT INTO notifications (user_id, actor_id, type, repo_id, owner_name, repo_name, subject_id, subject_url)
		 VALUES ($1, $1, 'issue_comment', $2, $3, 'core', 1, $4)`,
		env.ownerID, env.repoID, oldName, "/"+oldName+"/core/issues/1")

	if err := env.orgs.RenameOrg(ctx, env.org, env.ownerID, newName); err != nil {
		t.Fatal(err)
	}

	var eventOwner, noteOwner, noteURL string
	if err := env.db.QueryRow(`SELECT owner_name FROM events WHERE repo_id = $1`, env.repoID).Scan(&eventOwner); err != nil {
		t.Fatal(err)
	}
	if err := env.db.QueryRow(`SELECT owner_name, subject_url FROM notifications WHERE repo_id = $1`, env.repoID).Scan(&noteOwner, &noteURL); err != nil {
		t.Fatal(err)
	}
	if eventOwner != newName || noteOwner != newName || noteURL != "/"+newName+"/core/issues/1" {
		t.Errorf("stored text = %q, %q, %q; want the new name", eventOwner, noteOwner, noteURL)
	}
}
