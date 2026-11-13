package service_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	czconfig "github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type forkPullEnv struct {
	root              string
	upOwner, upRepo   string
	upOwnerID         int64
	forkerID          int64
	forkOwner         string
	forkRepo          string
	pulls             *service.PullService
	repos             *service.RepoService
	mainHash, forkSHA plumbing.Hash
}

// newForkPullEnv forks an upstream with a `main` branch and gives the fork a `fix-x` branch one commit ahead of it.
func newForkPullEnv(t *testing.T) *forkPullEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	e := &forkPullEnv{root: t.TempDir()}
	e.upOwnerID = testutil.SeedUser(t, db, suffix)
	e.upOwner = "testuser_" + suffix
	e.upRepo = "testrepo_" + suffix
	testutil.SeedRepo(t, db, e.upOwnerID, e.upOwner, suffix)
	codeSvc, _ := seedBareRepo(t, e.root, e.upOwner, e.upRepo, "feature-"+suffix)

	e.forkerID = testutil.SeedUser(t, db, suffix+"_f")
	e.forkOwner = "testuser_" + suffix + "_f"
	e.repos = service.NewRepoService(
		store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db),
		nil, nil, czconfig.GitConfig{ReposRoot: e.root},
	)
	fork, err := e.repos.Fork(context.Background(), e.upOwner, e.upRepo, e.forkerID, e.forkOwner, service.ForkOptions{})
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	e.forkRepo = fork.Name

	forkGit, err := gogit.PlainOpen(filepath.Join(e.root, e.forkOwner, e.forkRepo+".git"))
	if err != nil {
		t.Fatalf("open fork: %v", err)
	}
	mainRef, err := forkGit.Reference(plumbing.NewBranchReferenceName("main"), true)
	if err != nil {
		t.Fatalf("fork main: %v", err)
	}
	e.mainHash = mainRef.Hash()
	e.forkSHA = testutil.WriteCommit(t, forkGit.Storer, "fork change", e.mainHash)
	if err := forkGit.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName("fix-x"), e.forkSHA)); err != nil {
		t.Fatalf("set fix-x: %v", err)
	}

	e.pulls = service.NewPullService(store.NewPullStore(db), store.NewRepoStore(db), e.repos).
		WithCIDeps(codeSvc, nil, nil, nil, nil, nil)
	return e
}

func TestPullCreate_FromFork_CopiesHeadIntoBase(t *testing.T) {
	e := newForkPullEnv(t)

	pr, err := e.pulls.CreateFromHead(context.Background(), e.upOwner, e.upRepo, e.forkOwner+"/"+e.forkRepo,
		e.forkerID, "Fix x", "", "fix-x", "main", false, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if pr.HeadSHA != e.forkSHA.String() {
		t.Errorf("head sha = %q, want %q", pr.HeadSHA, e.forkSHA)
	}
	if want := e.forkOwner + "/" + e.forkRepo; pr.HeadRepo != want {
		t.Errorf("head repo = %q, want %q", pr.HeadRepo, want)
	}
	base, err := gogit.PlainOpen(filepath.Join(e.root, e.upOwner, e.upRepo+".git"))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := base.Reference(plumbing.ReferenceName("refs/pull/1/head"), true)
	if err != nil {
		t.Fatalf("base has no refs/pull/1/head: %v", err)
	}
	if ref.Hash() != e.forkSHA {
		t.Errorf("refs/pull/1/head = %s, want %s", ref.Hash(), e.forkSHA)
	}
	if _, err := base.CommitObject(e.forkSHA); err != nil {
		t.Errorf("the fork's commit is not in the base's object store: %v", err)
	}
}

func TestPullCreate_FromFork_NeedsWriteOnHead(t *testing.T) {
	e := newForkPullEnv(t)

	_, err := e.pulls.CreateFromHead(context.Background(), e.upOwner, e.upRepo, e.forkOwner+"/"+e.forkRepo,
		e.upOwnerID, "Fix x", "", "fix-x", "main", false, nil)

	if !errors.Is(err, service.ErrPullForbidden) {
		t.Fatalf("an upstream owner with no write access to the fork: err = %v, want ErrPullForbidden", err)
	}
}

func TestPullCreate_HeadOutsideForkTree_Refused(t *testing.T) {
	e := newForkPullEnv(t)
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t) + "_x"
	strangerID := testutil.SeedUser(t, db, suffix)
	stranger := "testuser_" + suffix
	testutil.SeedRepo(t, db, strangerID, stranger, suffix)

	_, err := e.pulls.CreateFromHead(context.Background(), e.upOwner, e.upRepo, stranger+"/testrepo_"+suffix,
		strangerID, "Fix x", "", "main", "main", false, nil)

	if !errors.Is(err, service.ErrPullHeadNotInTree) {
		t.Fatalf("err = %v, want ErrPullHeadNotInTree", err)
	}
}
