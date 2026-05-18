package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	gitobj "github.com/go-git/go-git/v5/plumbing/object"

	czconfig "github.com/mkappworks-dev/cloudzilla-app/internal/config"
)

// Builds a bare repo whose only branch is main while its symbolic HEAD still points at a nonexistent master.
func newTestRepoStaleHEAD(t *testing.T, owner, name string) *CodeService {
	t.Helper()
	root := t.TempDir()

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
	wt, err := work.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "f.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if _, err := wt.Add("f.txt"); err != nil {
		t.Fatalf("add: %v", err)
	}
	sig := &gitobj.Signature{Name: "Ada", Email: "ada@example.com", When: time.Now()}
	if _, err := wt.Commit("initial", &gogit.CommitOptions{Author: sig, Committer: sig}); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if _, err := work.CreateRemote(&gitconfig.RemoteConfig{Name: "bare", URLs: []string{bareDir}}); err != nil {
		t.Fatalf("create remote: %v", err)
	}
	// Commits land on `main`; `master` is never created in the bare repo.
	if err := work.Push(&gogit.PushOptions{
		RemoteName: "bare",
		RefSpecs:   []gitconfig.RefSpec{"refs/heads/master:refs/heads/main"},
	}); err != nil {
		t.Fatalf("push: %v", err)
	}

	bare, err := gogit.PlainOpen(bareDir)
	if err != nil {
		t.Fatalf("open bare: %v", err)
	}
	// PlainInit already leaves HEAD -> refs/heads/master; pin it so the test
	// exercises the broken state explicitly.
	headRef := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("master"))
	if err := bare.Storer.SetReference(headRef); err != nil {
		t.Fatalf("set HEAD: %v", err)
	}

	return NewCodeService(czconfig.GitConfig{ReposRoot: root})
}

// GetContributors must walk the repo's default branch, not its symbolic HEAD:
// a stale HEAD pointing at a missing branch otherwise empties the panel.
func TestCodeService_GetContributors_ResolvesDefaultBranch(t *testing.T) {
	t.Parallel()
	svc := newTestRepoStaleHEAD(t, "ada", "proj")

	got, err := svc.GetContributors("ada", "proj", "main")
	if err != nil {
		t.Fatalf("GetContributors: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 contributor, got %d (%+v)", len(got), got)
	}
	if got[0].Name != "Ada" || got[0].Commits != 1 {
		t.Errorf("want Ada with 1 commit, got %+v", got[0])
	}
}
