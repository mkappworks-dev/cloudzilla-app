package service_test

// Integration test for PullService.ListWithCIStatus.
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
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func seedBareRepo(t *testing.T, root, owner, name, branchName string) (*service.CodeService, string) {
	t.Helper()

	bareDir := filepath.Join(root, owner, name+".git")
	if err := os.MkdirAll(filepath.Dir(bareDir), 0o755); err != nil {
		t.Fatalf("mkdir owner: %v", err)
	}
	if _, err := gogit.PlainInit(bareDir, true); err != nil {
		t.Fatalf("plain init bare: %v", err)
	}

	workDir := t.TempDir()
	work, err := gogit.PlainInit(workDir, false)
	if err != nil {
		t.Fatalf("plain init work: %v", err)
	}
	if _, err := work.CreateRemote(&gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{bareDir},
	}); err != nil {
		t.Fatalf("create remote: %v", err)
	}

	// Force HEAD to point at "main" before the first commit.
	mainRef := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main"))
	if err := work.Storer.SetReference(mainRef); err != nil {
		t.Fatalf("set HEAD to main: %v", err)
	}

	wt, err := work.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	sig := &gitobj.Signature{Name: "Tester", Email: "t@test.invalid", When: time.Now().UTC()}

	writeAndCommit := func(filename, content, msg string) plumbing.Hash {
		t.Helper()
		full := filepath.Join(workDir, filename)
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", filename, err)
		}
		if _, err := wt.Add(filename); err != nil {
			t.Fatalf("add %s: %v", filename, err)
		}
		h, err := wt.Commit(msg, &gogit.CommitOptions{Author: sig, Committer: sig})
		if err != nil {
			t.Fatalf("commit %s: %v", msg, err)
		}
		return h
	}

	writeAndCommit("README.md", "init\n", "init")

	if err := wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branchName),
		Create: true,
	}); err != nil {
		t.Fatalf("create branch %s: %v", branchName, err)
	}
	headHash := writeAndCommit("feature.txt", "feature\n", "feat: add feature")

	push := func(branch string) {
		t.Helper()
		refspec := gitconfig.RefSpec("refs/heads/" + branch + ":refs/heads/" + branch)
		if err := work.Push(&gogit.PushOptions{
			RemoteName: "origin",
			RefSpecs:   []gitconfig.RefSpec{refspec},
		}); err != nil {
			t.Fatalf("push %s: %v", branch, err)
		}
	}
	push("main")
	push(branchName)

	codeSvc := service.NewCodeService(czconfig.GitConfig{ReposRoot: root})
	return codeSvc, headHash.String()
}

func TestPullService_ListWithCIStatus(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoName := "testrepo_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)

	reposRoot := t.TempDir()
	branchName := "feature-" + suffix
	codeSvc, headSHA := seedBareRepo(t, reposRoot, ownerName, repoName, branchName)

	repoSvc := service.NewRepoService(
		store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db),
		nil, nil, nil, czconfig.GitConfig{},
	)
	pullStore := store.NewPullStore(db)
	svc := service.NewPullService(pullStore, store.NewRepoStore(db), repoSvc)

	ctx := context.Background()
	pr, err := svc.Create(ctx, ownerName, repoName, ownerID,
		"CI test PR", "", branchName, "main", false)
	if err != nil {
		t.Fatalf("Create PR: %v", err)
	}
	_ = pr

	csStore := store.NewCommitStatusStore(db)
	for _, cs := range []struct {
		ctx   string
		state model.CommitStatusState
	}{
		{"ci/build", model.CommitStatusSuccess},
		{"ci/test", model.CommitStatusFailure},
	} {
		if err := csStore.Upsert(ctx, &model.CommitStatus{
			RepoID:    repoID,
			SHA:       headSHA,
			Context:   cs.ctx,
			State:     cs.state,
			CreatorID: ownerID,
		}); err != nil {
			t.Fatalf("seed commit status %s: %v", cs.ctx, err)
		}
	}

	commitStatusSvc := service.NewCommitStatusService(
		csStore, store.NewRepoStore(db), pullStore, nil, codeSvc,
	)
	fullSvc := service.NewPullService(pullStore, store.NewRepoStore(db), repoSvc).WithCIDeps(
		codeSvc, commitStatusSvc,
		store.NewPullReviewStore(db),
		store.NewLabelStore(db),
		store.NewAssigneeStore(db),
	)

	rows, err := fullSvc.ListWithCIStatus(ctx, ownerName, repoName, model.PRStateOpen, 0, 50)
	if err != nil {
		t.Fatalf("ListWithCIStatus: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].CIStatus != "failure" {
		t.Errorf("expected combined CI status 'failure', got %q", rows[0].CIStatus)
	}
}
