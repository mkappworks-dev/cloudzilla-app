package service_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"

	czconfig "github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
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
	db                *sql.DB
	fork              *model.Repository
	pulls             *service.PullService
	code              *service.CodeService
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
	e.fork = fork
	e.db = db

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

	e.code = codeSvc
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

func TestForkPull_ReadsThroughItsHeadRef(t *testing.T) {
	e := newForkPullEnv(t)
	ctx := context.Background()
	pr, err := e.pulls.CreateFromHead(ctx, e.upOwner, e.upRepo, e.forkOwner+"/"+e.forkRepo,
		e.forkerID, "Fix x", "", "fix-x", "main", false, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := e.pulls.Get(ctx, e.upOwner, e.upRepo, pr.Number)
	if err != nil {
		t.Fatal(err)
	}
	head := got.HeadRef()

	mg, err := e.code.Mergeability(ctx, e.upOwner, e.upRepo, "main", head)
	if err != nil {
		t.Fatalf("mergeability: %v", err)
	}
	if mg.Ahead != 1 || mg.Behind != 0 || mg.HasConflicts {
		t.Errorf("mergeability = ahead %d, behind %d, conflicts %v; want 1, 0, false", mg.Ahead, mg.Behind, mg.HasConflicts)
	}
	commits, err := e.code.PullCommits(e.upOwner, e.upRepo, "main", head)
	if err != nil {
		t.Fatalf("commits: %v", err)
	}
	if len(commits) != 1 || commits[0].Message != "fork change" {
		t.Errorf("commits = %+v, want the one fork commit", commits)
	}
	if _, err := e.code.GetPullDiff(e.upOwner, e.upRepo, "main", head); err != nil {
		t.Errorf("diff: %v", err)
	}
}

func TestPullHeadRef(t *testing.T) {
	same := &model.PullRequest{Number: 3, HeadBranch: "feature"}
	cross := &model.PullRequest{Number: 3, HeadBranch: "feature", CrossRepo: true}
	if got := same.HeadRef(); got != "feature" {
		t.Errorf("same-repo head ref = %q, want the branch", got)
	}
	if got := cross.HeadRef(); got != "refs/pull/3/head" {
		t.Errorf("cross-repo head ref = %q, want refs/pull/3/head", got)
	}
}

func TestForkPull_BehindWhenBaseMovesOn(t *testing.T) {
	e := newForkPullEnv(t)
	ctx := context.Background()
	pr, err := e.pulls.CreateFromHead(ctx, e.upOwner, e.upRepo, e.forkOwner+"/"+e.forkRepo,
		e.forkerID, "Fix x", "", "fix-x", "main", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	base, err := gogit.PlainOpen(filepath.Join(e.root, e.upOwner, e.upRepo+".git"))
	if err != nil {
		t.Fatal(err)
	}
	mainCommit, err := base.CommitObject(e.mainHash)
	if err != nil {
		t.Fatal(err)
	}
	sig := object.Signature{Name: "Tester", Email: "t@test.invalid", When: time.Unix(1, 0).UTC()}
	// Same tree as main, so upstream moves on without touching a path the fork changed.
	moved := &object.Commit{Author: sig, Committer: sig, Message: "upstream moves on", TreeHash: mainCommit.TreeHash, ParentHashes: []plumbing.Hash{e.mainHash}}
	enc := base.Storer.NewEncodedObject()
	if err := moved.Encode(enc); err != nil {
		t.Fatal(err)
	}
	next, err := base.Storer.SetEncodedObject(enc)
	if err != nil {
		t.Fatal(err)
	}
	if err := base.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), next)); err != nil {
		t.Fatal(err)
	}

	mg, err := e.code.Mergeability(ctx, e.upOwner, e.upRepo, "main", "refs/pull/"+itoa(pr.Number)+"/head")
	if err != nil {
		t.Fatal(err)
	}

	if mg.Ahead != 1 || mg.Behind != 1 || mg.HasConflicts {
		t.Errorf("mergeability = ahead %d, behind %d, conflicts %v; want 1, 1, false", mg.Ahead, mg.Behind, mg.HasConflicts)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// pushToFork moves the fork's fix-x to a new commit on top of its tip, as a push would, and runs the post-receive hook.
func (e *forkPullEnv) pushToFork(t *testing.T, branch string, force bool) plumbing.Hash {
	t.Helper()
	forkGit, err := gogit.PlainOpen(filepath.Join(e.root, e.forkOwner, e.forkRepo+".git"))
	if err != nil {
		t.Fatal(err)
	}
	ref := plumbing.NewBranchReferenceName(branch)
	old, err := forkGit.Reference(ref, true)
	if err != nil {
		t.Fatal(err)
	}
	parent := old.Hash()
	if force {
		parent = e.mainHash
	}
	next := testutil.WriteCommit(t, forkGit.Storer, "second change "+time.Now().String(), parent)
	if err := forkGit.Storer.SetReference(plumbing.NewHashReference(ref, next)); err != nil {
		t.Fatal(err)
	}
	repos := service.NewRepoService(store.NewRepoStore(e.db), store.NewUserStore(e.db), store.NewOrgStore(e.db),
		nil, e.code, czconfig.GitConfig{ReposRoot: e.root}).WithPullStore(store.NewPullStore(e.db))
	cmd := &packp.Command{Name: ref, Old: old.Hash(), New: next}
	if err := repos.OnPostReceive(context.Background(), e.fork, forkGit, []*packp.Command{cmd}); err != nil {
		t.Fatalf("post-receive: %v", err)
	}
	return next
}

func TestForkPush_UpdatesTheOpenPull(t *testing.T) {
	for _, force := range []bool{false, true} {
		name := map[bool]string{false: "push", true: "force-push"}[force]
		t.Run(name, func(t *testing.T) {
			e := newForkPullEnv(t)
			ctx := context.Background()
			pr, err := e.pulls.CreateFromHead(ctx, e.upOwner, e.upRepo, e.forkOwner+"/"+e.forkRepo,
				e.forkerID, "Fix x", "", "fix-x", "main", false, nil)
			if err != nil {
				t.Fatal(err)
			}

			next := e.pushToFork(t, "fix-x", force)

			got, err := e.pulls.Get(ctx, e.upOwner, e.upRepo, pr.Number)
			if err != nil {
				t.Fatal(err)
			}
			if got.HeadSHA != next.String() {
				t.Errorf("head sha = %q, want %s", got.HeadSHA, next)
			}
			base, err := gogit.PlainOpen(filepath.Join(e.root, e.upOwner, e.upRepo+".git"))
			if err != nil {
				t.Fatal(err)
			}
			ref, err := base.Reference(service.PullHeadRef(pr.Number), true)
			if err != nil || ref.Hash() != next {
				t.Errorf("base %s = %v (err %v), want %s", service.PullHeadRef(pr.Number), ref, err, next)
			}
			if _, err := base.CommitObject(next); err != nil {
				t.Errorf("the new commit is not in the base's object store: %v", err)
			}
		})
	}
}

// The store matches a pushed branch to pull requests by head repository, so a branch of the base that shares a fork
// pull request's head branch name leaves it alone.
func TestUpdateHeadSHAByBranch_SkipsAForkPullWithTheSameBranchName(t *testing.T) {
	e := newForkPullEnv(t)
	ctx := context.Background()
	pr, err := e.pulls.CreateFromHead(ctx, e.upOwner, e.upRepo, e.forkOwner+"/"+e.forkRepo,
		e.forkerID, "Fix x", "", "fix-x", "main", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	up, err := e.repos.Get(ctx, e.upOwner, e.upRepo)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.NewPullStore(e.db).UpdateHeadSHAByBranch(ctx, up.ID, "fix-x", "deadbeef"); err != nil {
		t.Fatal(err)
	}

	got, err := e.pulls.Get(ctx, e.upOwner, e.upRepo, pr.Number)
	if err != nil {
		t.Fatal(err)
	}
	if got.HeadSHA != e.forkSHA.String() {
		t.Errorf("head sha = %q, want it unchanged at %s", got.HeadSHA, e.forkSHA)
	}
}

func TestForkPush_CopyFailureKeepsTheOldHead(t *testing.T) {
	e := newForkPullEnv(t)
	ctx := context.Background()
	pr, err := e.pulls.CreateFromHead(ctx, e.upOwner, e.upRepo, e.forkOwner+"/"+e.forkRepo,
		e.forkerID, "Fix x", "", "fix-x", "main", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(e.root, e.forkOwner, e.forkRepo+".git")); err != nil {
		t.Fatal(err)
	}
	repos := service.NewRepoService(store.NewRepoStore(e.db), store.NewUserStore(e.db), store.NewOrgStore(e.db),
		nil, e.code, czconfig.GitConfig{ReposRoot: e.root}).WithPullStore(store.NewPullStore(e.db))

	err = repos.OnPostReceive(ctx, e.fork, nil, []*packp.Command{{Name: plumbing.NewBranchReferenceName("fix-x"), New: e.mainHash}})

	if err != nil {
		t.Fatalf("a failed copy must not fail the push: %v", err)
	}
	got, err := e.pulls.Get(ctx, e.upOwner, e.upRepo, pr.Number)
	if err != nil {
		t.Fatal(err)
	}
	if got.HeadSHA != e.forkSHA.String() {
		t.Errorf("head sha = %q, want it kept at %s", got.HeadSHA, e.forkSHA)
	}
}
