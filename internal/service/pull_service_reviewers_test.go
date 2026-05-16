package service_test

// Requires TEST_DATABASE_DSN and skips otherwise.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	gitobj "github.com/go-git/go-git/v5/plumbing/object"

	czconfig "github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// seedRepoWithCODEOWNERS initialises a bare repo with:
//   - main: README.md + CODEOWNERS commited
//   - featureBranch: main.go added on top
//
// Returns the CodeService pointed at reposRoot.
func seedRepoWithCODEOWNERS(t *testing.T, reposRoot, owner, name, featureBranch, codeowners string) *service.CodeService {
	t.Helper()

	bareDir := filepath.Join(reposRoot, owner, name+".git")
	if err := os.MkdirAll(filepath.Dir(bareDir), 0o755); err != nil {
		t.Fatalf("mkdir owner dir: %v", err)
	}
	if _, err := gogit.PlainInit(bareDir, true); err != nil {
		t.Fatalf("init bare repo: %v", err)
	}

	workDir := t.TempDir()
	work, err := gogit.PlainInit(workDir, false)
	if err != nil {
		t.Fatalf("init work repo: %v", err)
	}
	if _, err := work.CreateRemote(&gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{bareDir},
	}); err != nil {
		t.Fatalf("create remote: %v", err)
	}

	mainRef := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main"))
	if err := work.Storer.SetReference(mainRef); err != nil {
		t.Fatalf("set HEAD to main: %v", err)
	}

	wt, err := work.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	sig := &gitobj.Signature{Name: "Tester", Email: "t@test.invalid", When: time.Now().UTC()}

	writeAndCommit := func(filename, content, msg string) {
		t.Helper()
		full := filepath.Join(workDir, filename)
		if dir := filepath.Dir(full); dir != workDir {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", dir, err)
			}
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", filename, err)
		}
		if _, err := wt.Add(filename); err != nil {
			t.Fatalf("git add %s: %v", filename, err)
		}
		if _, err := wt.Commit(msg, &gogit.CommitOptions{Author: sig, Committer: sig}); err != nil {
			t.Fatalf("commit %q: %v", msg, err)
		}
	}

	writeAndCommit("README.md", "init\n", "init")
	writeAndCommit("CODEOWNERS", codeowners, "add CODEOWNERS")

	push := func(branch string) {
		t.Helper()
		refspec := gitconfig.RefSpec("refs/heads/" + branch + ":refs/heads/" + branch)
		if err := work.Push(&gogit.PushOptions{RemoteName: "origin", RefSpecs: []gitconfig.RefSpec{refspec}}); err != nil {
			t.Fatalf("push %s: %v", branch, err)
		}
	}
	push("main")

	if err := wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(featureBranch),
		Create: true,
	}); err != nil {
		t.Fatalf("checkout %s: %v", featureBranch, err)
	}
	writeAndCommit("main.go", "package main\n", "add main.go")
	push(featureBranch)

	return service.NewCodeService(czconfig.GitConfig{ReposRoot: reposRoot})
}

func TestSuggestReviewers_FallsBackToContributors(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)

	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)

	// Seed a second user who is the top contributor.
	topSuffix := suffix + "_top"
	topID := testutil.SeedUser(t, db, topSuffix)
	topUsername := "testuser_" + topSuffix

	ctx := context.Background()
	contribStore := store.NewContributorStatsStore(db)
	week := time.Now().UTC()

	// topID has more commits than ownerID.
	if err := contribStore.UpsertStats(ctx, repoID, topID, week, 50, 0, 0); err != nil {
		t.Fatalf("seed top contributor: %v", err)
	}
	if err := contribStore.UpsertStats(ctx, repoID, ownerID, week, 5, 0, 0); err != nil {
		t.Fatalf("seed owner contributor: %v", err)
	}

	repoStore := store.NewRepoStore(db)
	userStore := store.NewUserStore(db)
	repoSvc := service.NewRepoService(
		repoStore, userStore, store.NewOrgStore(db),
		nil, nil, nil, czconfig.GitConfig{},
	)

	// No CodeService — forces the contributor fallback.
	pullSvc := service.NewPullService(store.NewPullStore(db), repoStore, repoSvc).
		WithReviewerDeps(contribStore, userStore)

	out, err := pullSvc.SuggestReviewers(ctx, ownerName, "testrepo_"+suffix, "main", "feature", 3)
	if err != nil {
		t.Fatalf("SuggestReviewers: %v", err)
	}
	var found bool
	for _, u := range out {
		if u.Username == topUsername {
			found = true
		}
	}
	if !found {
		t.Errorf("expected top contributor %q in results, got %+v", topUsername, out)
	}
}

func TestSuggestReviewers_PrefersCodeOwners(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)

	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoName := "testrepo_" + suffix
	_ = testutil.SeedRepo(t, db, ownerID, ownerName, suffix)

	// Seed "bob" — the CODEOWNERS assignee for *.go files.
	bobSuffix := suffix + "_bob"
	bobID := testutil.SeedUser(t, db, bobSuffix)
	_ = bobID

	// Update bob's username in the DB to a predictable value so CODEOWNERS can reference it.
	bobUsername := "bob_" + suffix
	if _, err := db.ExecContext(context.Background(),
		`UPDATE users SET username=$1 WHERE id=$2`, bobUsername, bobID); err != nil {
		t.Fatalf("rename bob: %v", err)
	}

	reposRoot := t.TempDir()
	featureBranch := "feature-" + suffix
	codeowners := "*.go @" + bobUsername + "\n"
	codeSvc := seedRepoWithCODEOWNERS(t, reposRoot, ownerName, repoName, featureBranch, codeowners)

	repoStore := store.NewRepoStore(db)
	userStore := store.NewUserStore(db)
	repoSvc := service.NewRepoService(
		repoStore, userStore, store.NewOrgStore(db),
		nil, nil, nil, czconfig.GitConfig{},
	)

	// Update the repo's default_branch to "main" so GetCodeOwners reads from the right ref.
	if _, err := db.ExecContext(context.Background(),
		`UPDATE repositories SET default_branch='main' WHERE owner_name=$1 AND name=$2`,
		ownerName, repoName); err != nil {
		t.Fatalf("set default_branch: %v", err)
	}

	pullSvc := service.NewPullService(store.NewPullStore(db), repoStore, repoSvc).
		WithCIDeps(codeSvc, nil, nil, nil, nil).
		WithReviewerDeps(store.NewContributorStatsStore(db), userStore)

	ctx := context.Background()
	out, err := pullSvc.SuggestReviewers(ctx, ownerName, repoName, "main", featureBranch, 3)
	if err != nil {
		t.Fatalf("SuggestReviewers: %v", err)
	}
	var found bool
	for _, u := range out {
		if u.Username == bobUsername {
			found = true
		}
	}
	if !found {
		t.Errorf("expected %q from CODEOWNERS, got %+v", bobUsername, out)
	}
}
