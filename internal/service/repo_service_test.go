package service_test

// Integration tests for RepoService.Create with repository initialization.
// All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	gitobj "github.com/go-git/go-git/v5/plumbing/object"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// newRepoSvc builds a RepoService backed by the test database with a
// per-test ReposRoot, seeding an owner user. Returns the service, owner
// username, and the filesystem ReposRoot.
func newRepoSvc(t *testing.T) (*service.RepoService, string, string) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	root := t.TempDir()
	svc := service.NewRepoService(
		store.NewRepoStore(db),
		store.NewUserStore(db),
		store.NewOrgStore(db),
		nil, nil, nil,
		config.GitConfig{ReposRoot: root},
	)
	return svc, ownerName, root
}

// bareTreeFiles opens the bare repo at bareDir, resolves branch, and returns
// the set of file paths in that branch's tree.
func bareTreeFiles(t *testing.T, bareDir, branch string) map[string]bool {
	t.Helper()
	repo, err := gogit.PlainOpen(bareDir)
	if err != nil {
		t.Fatalf("open bare repo: %v", err)
	}
	ref, err := repo.Reference(plumbing.NewBranchReferenceName(branch), true)
	if err != nil {
		t.Fatalf("resolve branch %s: %v", branch, err)
	}
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("commit object: %v", err)
	}
	tree, err := commit.Tree()
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	files := map[string]bool{}
	if err := tree.Files().ForEach(func(f *gitobj.File) error {
		files[f.Name] = true
		return nil
	}); err != nil {
		t.Fatalf("walk tree: %v", err)
	}
	return files
}

func TestRepoService_Create_WithInitFiles(t *testing.T) {
	svc, owner, root := newRepoSvc(t)
	ctx := context.Background()

	repo, err := svc.Create(ctx, owner, "initrepo", "an initialized project", false, service.RepoInitOptions{
		AddREADME: true,
		Gitignore: "Go",
		License:   "mit",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	bareDir := filepath.Join(root, owner, repo.Name+".git")
	files := bareTreeFiles(t, bareDir, repo.DefaultBranch)

	for _, want := range []string{"README.md", ".gitignore", "LICENSE"} {
		if !files[want] {
			t.Errorf("initial commit missing %s (have %v)", want, files)
		}
	}

	// Bare HEAD must point at the default branch.
	bare, err := gogit.PlainOpen(bareDir)
	if err != nil {
		t.Fatalf("open bare: %v", err)
	}
	head, err := bare.Reference(plumbing.HEAD, false)
	if err != nil {
		t.Fatalf("read HEAD: %v", err)
	}
	wantHead := plumbing.NewBranchReferenceName(repo.DefaultBranch)
	if head.Target() != wantHead {
		t.Errorf("bare HEAD: want %s, got %s", wantHead, head.Target())
	}
}

func TestRepoService_Create_NoInit(t *testing.T) {
	svc, owner, root := newRepoSvc(t)
	ctx := context.Background()

	repo, err := svc.Create(ctx, owner, "emptyrepo", "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	bareDir := filepath.Join(root, owner, repo.Name+".git")
	bare, err := gogit.PlainOpen(bareDir)
	if err != nil {
		t.Fatalf("open bare: %v", err)
	}
	if _, err := bare.Reference(plumbing.NewBranchReferenceName(repo.DefaultBranch), true); err == nil {
		t.Errorf("empty repo should have no %s ref", repo.DefaultBranch)
	}
	iter, err := bare.CommitObjects()
	if err != nil {
		t.Fatalf("commit objects: %v", err)
	}
	defer iter.Close()
	if _, err := iter.Next(); err == nil {
		t.Error("empty repo should have no commits")
	}
}
