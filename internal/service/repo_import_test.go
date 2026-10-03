package service_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newImportRepoSvc(t *testing.T) (*service.RepoService, *service.OrgService, *sql.DB, string) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	cfg := config.GitConfig{ReposRoot: t.TempDir()}
	repos, users, orgs := store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db)
	return service.NewRepoService(repos, users, orgs, nil, nil, cfg), service.NewOrgService(orgs, repos, users, cfg), db, cfg.ReposRoot
}

// seedImportUser's repos are deleted before the user: cleanups run last-in, first-out.
func seedImportUser(t *testing.T, db *sql.DB) (int64, string) {
	t.Helper()
	suffix := testutil.UniqueSuffix(t)
	id := testutil.SeedUser(t, db, suffix)
	name := "testuser_" + suffix
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM repositories WHERE owner_name = $1`, name) })
	return id, name
}

func seedImportedClone(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "clone")
	repo, err := gogit.PlainInit(dir, true)
	if err != nil {
		t.Fatalf("init clone: %v", err)
	}
	c := testutil.WriteCommit(t, repo.Storer, "first")
	if err := repo.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName("trunk"), c)); err != nil {
		t.Fatalf("set trunk: %v", err)
	}
	return dir
}

func seedImportOrg(t *testing.T, db *sql.DB, orgs *service.OrgService, ownerID int64) string {
	t.Helper()
	org, err := orgs.Create(context.Background(), ownerID, "imporg_"+testutil.UniqueSuffix(t), "", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	testutil.DeleteOrgOnCleanup(t, db, org.ID)
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM repositories WHERE owner_name = $1`, org.Name) })
	return org.Name
}

func TestResolveImportTarget_OwnAccountAndOwnedOrg(t *testing.T) {
	svc, orgs, db, _ := newImportRepoSvc(t)
	ctx := context.Background()
	uid, uname := seedImportUser(t, db)

	got, err := svc.ResolveImportTarget(ctx, uid, uname, "")
	if want := (service.ImportTarget{ActorID: uid, OwnerName: uname, OwnerID: uid}); err != nil || got != want {
		t.Errorf("own account: got %+v, %v; want %+v", got, err, want)
	}

	orgName := seedImportOrg(t, db, orgs, uid)
	got, err = svc.ResolveImportTarget(ctx, uid, uname, orgName)
	if err != nil || got.OwnerName != orgName || got.OrgID == 0 || got.OwnerID != 0 || got.ActorID != uid {
		t.Errorf("owned org: got %+v, %v", got, err)
	}
}

func TestResolveImportTarget_RefusesOrgsTheActorDoesNotOwn(t *testing.T) {
	svc, orgs, db, _ := newImportRepoSvc(t)
	ctx := context.Background()
	ownerID, _ := seedImportUser(t, db)
	otherID, otherName := seedImportUser(t, db)
	orgName := seedImportOrg(t, db, orgs, ownerID)

	for _, owner := range []string{orgName, "no-such-org-" + testutil.UniqueSuffix(t)} {
		if _, err := svc.ResolveImportTarget(ctx, otherID, otherName, owner); !errors.Is(err, service.ErrForbidden) {
			t.Errorf("owner %q: err = %v, want ErrForbidden", owner, err)
		}
	}
}

func TestCheckImportName(t *testing.T) {
	svc, _, db, _ := newImportRepoSvc(t)
	ctx := context.Background()
	uid, uname := seedImportUser(t, db)
	if _, err := svc.Create(ctx, uid, uname, "taken", "", false, service.RepoInitOptions{}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.CheckImportName(ctx, uname, "free"); err != nil {
		t.Errorf("free name: %v", err)
	}
	if err := svc.CheckImportName(ctx, uname, "taken"); !errors.Is(err, service.ErrRepoNameTaken) {
		t.Errorf("taken name: err = %v, want ErrRepoNameTaken", err)
	}
	if err := svc.CheckImportName(ctx, uname, "bad name"); !errors.Is(err, service.ErrInvalidRepoName) {
		t.Errorf("invalid name: err = %v, want ErrInvalidRepoName", err)
	}
}

func TestCreateFromImport_AdoptsTheClone(t *testing.T) {
	svc, _, db, root := newImportRepoSvc(t)
	ctx := context.Background()
	uid, uname := seedImportUser(t, db)
	src := seedImportedClone(t)
	target := service.ImportTarget{ActorID: uid, OwnerName: uname, OwnerID: uid}

	repo, err := svc.CreateFromImport(ctx, target, "imported", "from elsewhere", true, "trunk", src)
	if err != nil {
		t.Fatalf("CreateFromImport: %v", err)
	}
	got, err := svc.Get(ctx, uname, "imported")
	if err != nil || got.ID != repo.ID || got.DefaultBranch != "trunk" || !got.Private || got.Description != "from elsewhere" {
		t.Errorf("row = %+v, %v", got, err)
	}
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("clone dir still exists: %v", err)
	}
	moved, err := gogit.PlainOpen(filepath.Join(root, uname, "imported.git"))
	if err != nil {
		t.Fatalf("open published repo: %v", err)
	}
	if _, err := moved.Reference(plumbing.NewBranchReferenceName("trunk"), false); err != nil {
		t.Errorf("trunk missing from published repo: %v", err)
	}
}

func TestCreateFromImport_NameTakenLeavesCloneForTheCaller(t *testing.T) {
	svc, _, db, _ := newImportRepoSvc(t)
	ctx := context.Background()
	uid, uname := seedImportUser(t, db)
	if _, err := svc.Create(ctx, uid, uname, "taken", "", false, service.RepoInitOptions{}); err != nil {
		t.Fatalf("create: %v", err)
	}
	src := seedImportedClone(t)
	target := service.ImportTarget{ActorID: uid, OwnerName: uname, OwnerID: uid}

	if _, err := svc.CreateFromImport(ctx, target, "taken", "", false, "trunk", src); !errors.Is(err, service.ErrRepoNameTaken) {
		t.Errorf("err = %v, want ErrRepoNameTaken", err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("clone dir removed: %v", err)
	}
}

func TestCreateFromImport_RechecksOrgOwnership(t *testing.T) {
	svc, orgs, db, _ := newImportRepoSvc(t)
	ctx := context.Background()
	ownerID, ownerName := seedImportUser(t, db)
	otherID, _ := seedImportUser(t, db)
	orgName := seedImportOrg(t, db, orgs, ownerID)
	target, err := svc.ResolveImportTarget(ctx, ownerID, ownerName, orgName)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	target.ActorID = otherID

	if _, err := svc.CreateFromImport(ctx, target, "imported", "", false, "trunk", seedImportedClone(t)); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("err = %v, want ErrForbidden", err)
	}
}
