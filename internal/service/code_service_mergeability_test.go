package service

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
)

// mergeabilityTestRepo seeds a bare repo at <root>/<owner>/<name>.git via
// a non-bare working repo + push, mirroring the helper pattern in
// code_service_log_test.go and code_service_walk_test.go. The caller
// drives the commits/branches against the returned worktree, then calls
// pushAll to mirror refs into the bare repo (where production code reads
// from). Returns (CodeService, work, workDir, bareDir).
func mergeabilityTestRepo(t *testing.T, owner, name string) (*CodeService, *gogit.Repository, string, string) {
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
	if _, err := work.CreateRemote(&gitconfig.RemoteConfig{
		Name: "bare",
		URLs: []string{bareDir},
	}); err != nil {
		t.Fatalf("create remote: %v", err)
	}

	return NewCodeService(czconfig.GitConfig{ReposRoot: root}), work, workDir, bareDir
}

// commitFile writes content to relPath, stages it, and creates a commit.
// Returns the commit hash.
func commitFile(t *testing.T, work *gogit.Repository, workDir, relPath, content, msg string) plumbing.Hash {
	t.Helper()
	full := filepath.Join(workDir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", relPath, err)
	}
	wt, err := work.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if _, err := wt.Add(relPath); err != nil {
		t.Fatalf("add %s: %v", relPath, err)
	}
	sig := &gitobj.Signature{Name: "Tester", Email: "tester@example.com", When: time.Now().UTC()}
	h, err := wt.Commit(msg, &gogit.CommitOptions{Author: sig, Committer: sig})
	if err != nil {
		t.Fatalf("commit %s: %v", msg, err)
	}
	return h
}

// checkout switches the worktree to an existing branch.
func checkout(t *testing.T, work *gogit.Repository, branch string) {
	t.Helper()
	wt, err := work.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if err := wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branch),
	}); err != nil {
		t.Fatalf("checkout %s: %v", branch, err)
	}
}

// createBranch creates a new branch off the current HEAD and switches to it.
func createBranch(t *testing.T, work *gogit.Repository, branch string) {
	t.Helper()
	wt, err := work.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if err := wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branch),
		Create: true,
	}); err != nil {
		t.Fatalf("create branch %s: %v", branch, err)
	}
}

// pushBranch pushes a single local branch to the bare repo under the same name.
func pushBranch(t *testing.T, work *gogit.Repository, branch string) {
	t.Helper()
	refspec := gitconfig.RefSpec("refs/heads/" + branch + ":refs/heads/" + branch)
	if err := work.Push(&gogit.PushOptions{
		RemoteName: "bare",
		RefSpecs:   []gitconfig.RefSpec{refspec},
	}); err != nil {
		t.Fatalf("push %s: %v", branch, err)
	}
}

// renameDefaultToMain switches the working repo's default branch from
// go-git's PlainInit default (master) to main, so the first commit lives
// on `main`.
func renameDefaultToMain(t *testing.T, work *gogit.Repository) {
	t.Helper()
	headRef := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main"))
	if err := work.Storer.SetReference(headRef); err != nil {
		t.Fatalf("rename default to main: %v", err)
	}
}

func TestMergeability_Clean(t *testing.T) {
	t.Parallel()
	svc, work, workDir, _ := mergeabilityTestRepo(t, "alice", "clean")
	renameDefaultToMain(t, work)

	// Initial commit on main.
	commitFile(t, work, workDir, "README.md", "v0\n", "init")

	// Branch off main → feature, add 2 commits modifying feature-only files.
	createBranch(t, work, "feature")
	commitFile(t, work, workDir, "feature_a.txt", "a\n", "feature: add a")
	commitFile(t, work, workDir, "feature_b.txt", "b\n", "feature: add b")

	// Back to main, add 1 commit modifying a different file.
	checkout(t, work, "main")
	commitFile(t, work, workDir, "main_only.txt", "m\n", "main: add main_only")

	// Push both branches to the bare repo.
	pushBranch(t, work, "main")
	pushBranch(t, work, "feature")

	m, err := svc.Mergeability(context.Background(), "alice", "clean", "main", "feature")
	if err != nil {
		t.Fatalf("Mergeability: %v", err)
	}
	if m.HasConflicts {
		t.Errorf("expected no conflicts, got HasConflicts=true")
	}
	if m.Ahead != 2 {
		t.Errorf("expected Ahead=2, got %d", m.Ahead)
	}
	if m.Behind != 1 {
		t.Errorf("expected Behind=1, got %d", m.Behind)
	}
	if m.MergeBase == "" {
		t.Errorf("expected non-empty MergeBase SHA")
	}
	if m.BaseRef != "main" || m.HeadRef != "feature" {
		t.Errorf("ref echo wrong: base=%q head=%q", m.BaseRef, m.HeadRef)
	}
}

func TestMergeability_Conflicts(t *testing.T) {
	t.Parallel()
	svc, work, workDir, _ := mergeabilityTestRepo(t, "alice", "conflict")
	renameDefaultToMain(t, work)

	// Initial commit on main with the contested file.
	commitFile(t, work, workDir, "conflict.txt", "original\n", "init")

	// Branch off main → feature, modify conflict.txt.
	createBranch(t, work, "feature")
	commitFile(t, work, workDir, "conflict.txt", "feature version\n", "feature: rewrite")

	// Back to main, modify the same file differently.
	checkout(t, work, "main")
	commitFile(t, work, workDir, "conflict.txt", "main version\n", "main: rewrite")

	pushBranch(t, work, "main")
	pushBranch(t, work, "feature")

	m, err := svc.Mergeability(context.Background(), "alice", "conflict", "main", "feature")
	if err != nil {
		t.Fatalf("Mergeability: %v", err)
	}
	if !m.HasConflicts {
		t.Errorf("expected HasConflicts=true, got false (m=%+v)", m)
	}
	if m.MergeBase == "" {
		t.Errorf("expected non-empty MergeBase even on conflict")
	}
}

// TestMergeability_NoCommonAncestor is intentionally omitted. Seeding
// two truly-disjoint histories in a single repo requires direct Storer
// manipulation that fights go-git's worktree semantics and adds little
// value: the no-common-ancestor branch is exercised by inspection of
// findMergeBase's error path, which Mergeability surfaces by string
// match per the helper's contract.
