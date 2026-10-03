package service

import (
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestSeedInitialCommit_NeedsNoGitBinary(t *testing.T) {
	// The Docker image ships no git, and go-git's file transport execs git-receive-pack.
	t.Setenv("PATH", "")

	bareDir := filepath.Join(t.TempDir(), "proj.git")
	if _, err := gogit.PlainInit(bareDir, true); err != nil {
		t.Fatalf("init bare: %v", err)
	}
	sig := object.Signature{Name: "Alice", Email: "alice@example.com", When: time.Unix(1700000000, 0).UTC()}
	init := RepoInitOptions{AddREADME: true, Gitignore: "Go", License: "mit"}
	if err := seedInitialCommit(bareDir, "", sig, init, "alice", "proj", "A project"); err != nil {
		t.Fatalf("seedInitialCommit: %v", err)
	}

	repo, err := gogit.PlainOpen(bareDir)
	if err != nil {
		t.Fatalf("open bare: %v", err)
	}
	head, err := repo.Reference(plumbing.HEAD, false)
	if err != nil {
		t.Fatalf("read HEAD: %v", err)
	}
	if want := plumbing.NewBranchReferenceName("main"); head.Target() != want {
		t.Errorf("HEAD: want %s, got %s", want, head.Target())
	}
	tip, err := repo.Reference(plumbing.NewBranchReferenceName("main"), true)
	if err != nil {
		t.Fatalf("resolve main: %v", err)
	}
	commit, err := repo.CommitObject(tip.Hash())
	if err != nil {
		t.Fatalf("load commit: %v", err)
	}
	if commit.Message != "Initial commit" || len(commit.ParentHashes) != 0 {
		t.Errorf("commit: want a root commit %q, got %q with %d parents", "Initial commit", commit.Message, len(commit.ParentHashes))
	}
	for _, got := range []object.Signature{commit.Author, commit.Committer} {
		if got.Name != sig.Name || got.Email != sig.Email || !got.When.Equal(sig.When) {
			t.Errorf("commit signature: want %v, got author %v committer %v", sig, commit.Author, commit.Committer)
			break
		}
	}

	wantGitignore, _ := gitignoreContent("Go")
	wantLicense, _ := licenseContent("mit", "alice")
	want := map[string]string{
		"README.md":  "# proj\n\nA project\n",
		".gitignore": wantGitignore,
		"LICENSE":    wantLicense,
	}
	tree, err := commit.Tree()
	if err != nil {
		t.Fatalf("load tree: %v", err)
	}
	if len(tree.Entries) != len(want) {
		t.Errorf("tree: want %d entries, got %v", len(want), tree.Entries)
	}
	for name, content := range want {
		f, err := tree.File(name)
		if err != nil {
			t.Errorf("tree missing %s: %v", name, err)
			continue
		}
		if got, _ := f.Contents(); got != content {
			t.Errorf("%s: want %q, got %q", name, content, got)
		}
	}
}
