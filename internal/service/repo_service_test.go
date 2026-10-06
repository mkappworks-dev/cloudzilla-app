package service_test

import (
	"context"
	"fmt"
	"os"
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

func newRepoSvc(t *testing.T) (*service.RepoService, int64, string, string) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	root := t.TempDir()
	svc := service.NewRepoService(
		store.NewRepoStore(db),
		store.NewUserStore(db),
		store.NewOrgStore(db),
		nil, nil,
		config.GitConfig{ReposRoot: root},
	)
	return svc, ownerID, ownerName, root
}

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
	svc, ownerID, owner, root := newRepoSvc(t)
	ctx := context.Background()

	repo, err := svc.Create(ctx, ownerID, owner, "initrepo", "an initialized project", false, service.RepoInitOptions{
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
	svc, ownerID, owner, root := newRepoSvc(t)
	ctx := context.Background()

	repo, err := svc.Create(ctx, ownerID, owner, "emptyrepo", "", false, service.RepoInitOptions{})
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
	assertBareDirHead(t, bareDir, repo.DefaultBranch)
}

// An empty repo's HEAD names the branch the first push should create, and it
// must match the row's default_branch, not go-git's "master".
func assertBareDirHead(t *testing.T, bareDir, branch string) {
	t.Helper()
	bare, err := gogit.PlainOpen(bareDir)
	if err != nil {
		t.Fatalf("open bare: %v", err)
	}
	assertBareHead(t, bare, branch)
}

func TestRepoService_CreateFromTemplate_MissingSourceDir_HeadIsDefaultBranch(t *testing.T) {
	svc, ownerID, owner, root := newRepoSvc(t)
	ctx := context.Background()

	tmpl, err := svc.Create(ctx, ownerID, owner, "tmpl", "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatalf("Create template: %v", err)
	}
	if err := svc.SetTemplate(ctx, tmpl.ID, ownerID, true); err != nil {
		t.Fatalf("SetTemplate: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(root, owner, tmpl.Name+".git")); err != nil {
		t.Fatalf("remove template dir: %v", err)
	}

	repo, err := svc.CreateFromTemplate(ctx, tmpl.ID, ownerID, owner, "fromtmpl", "")
	if err != nil {
		t.Fatalf("CreateFromTemplate: %v", err)
	}
	assertBareDirHead(t, filepath.Join(root, owner, repo.Name+".git"), repo.DefaultBranch)
}

func TestRepoService_Create_InitCommitAuthorFollowsKeepEmailPrivate(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	owner := "testuser_" + suffix
	root := t.TempDir()
	svc := service.NewRepoService(store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db), nil, nil,
		config.GitConfig{ReposRoot: root}).WithNoreplyHostFrom("https://git.example.com")
	ctx := context.Background()

	initAuthor := func(name string) string {
		t.Helper()
		repo, err := svc.Create(ctx, userID, owner, name, "", false, service.RepoInitOptions{AddREADME: true})
		if err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		bare, err := gogit.PlainOpen(filepath.Join(root, owner, name+".git"))
		if err != nil {
			t.Fatalf("open bare: %v", err)
		}
		ref, err := bare.Reference(plumbing.NewBranchReferenceName(repo.DefaultBranch), true)
		if err != nil {
			t.Fatalf("resolve %s: %v", repo.DefaultBranch, err)
		}
		c, err := bare.CommitObject(ref.Hash())
		if err != nil {
			t.Fatalf("load commit: %v", err)
		}
		return c.Author.Email
	}

	if got, want := initAuthor("private_by_default"), fmt.Sprintf("%d+%s@users.noreply.git.example.com", userID, owner); got != want {
		t.Errorf("default: init commit author = %q, want %q", got, want)
	}
	if _, err := db.Exec(`UPDATE users SET keep_email_private = FALSE WHERE id = $1`, userID); err != nil {
		t.Fatalf("turn setting off: %v", err)
	}
	if got, want := initAuthor("public_email"), owner+"@test.invalid"; got != want {
		t.Errorf("setting off: init commit author = %q, want %q", got, want)
	}
}
