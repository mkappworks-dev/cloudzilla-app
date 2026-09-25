package service_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

// Builds <reposRoot>/<owner>/<repo>.git with two commits on main and one more on a feature branch forked from it.
func buildMultiBranchRepo(t *testing.T, owner, repoName string) string {
	t.Helper()
	reposRoot := t.TempDir()
	dir := filepath.Join(reposRoot, owner, repoName+".git")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	commit := func(file, content string) {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if _, err := wt.Add(file); err != nil {
			t.Fatalf("add: %v", err)
		}
		if _, err := wt.Commit("c "+file, &gogit.CommitOptions{
			Author: &object.Signature{Name: "T", Email: "t@test.invalid", When: time.Now()},
		}); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}
	commit("a.txt", "a\n")
	commit("b.txt", "b\n")
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if err := repo.Storer.SetReference(plumbing.NewHashReference("refs/heads/feature", head.Hash())); err != nil {
		t.Fatalf("set feature ref: %v", err)
	}
	if err := wt.Checkout(&gogit.CheckoutOptions{Branch: "refs/heads/feature"}); err != nil {
		t.Fatalf("checkout feature: %v", err)
	}
	commit("c.txt", "c\n")
	return reposRoot
}

func TestCodeService_WalkAllRefCommits_VisitsEveryRefDeduped(t *testing.T) {
	reposRoot := buildMultiBranchRepo(t, "alice", "proj")
	code := service.NewCodeService(config.GitConfig{ReposRoot: reposRoot})

	commits, err := code.WalkAllRefCommits("alice", "proj")
	if err != nil {
		t.Fatalf("WalkAllRefCommits: %v", err)
	}
	if len(commits) != 3 {
		t.Fatalf("want 3 unique commits, got %d: %+v", len(commits), commits)
	}
	seen := map[string]bool{}
	for _, c := range commits {
		if seen[c.SHA] {
			t.Errorf("duplicate SHA %s", c.SHA)
		}
		seen[c.SHA] = true
		if c.AuthorEmail != "t@test.invalid" {
			t.Errorf("author email: got %q", c.AuthorEmail)
		}
		if c.Additions < 1 {
			t.Errorf("commit %s: want additions >= 1, got %d", c.SHA, c.Additions)
		}
	}
}
