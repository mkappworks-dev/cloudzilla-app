package service

import (
	"context"
	"errors"
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

// newTestRepoWithFiles initializes a bare repo at <root>/<owner>/<name>.git
// and seeds it with a single commit containing the given files
// (path → content). Returns the CodeService configured to read from `root`.
// Mirrors newTestRepoWithCommits in code_service_log_test.go.
func newTestRepoWithFiles(t *testing.T, owner, name string, files map[string]string) *CodeService {
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

	for relPath, content := range files {
		full := filepath.Join(workDir, relPath)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir parent for %s: %v", relPath, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", relPath, err)
		}
		if _, err := wt.Add(relPath); err != nil {
			t.Fatalf("add %s: %v", relPath, err)
		}
	}

	sig := &gitobj.Signature{Name: "Tester", Email: "tester@example.com", When: time.Now().UTC()}
	if _, err := wt.Commit("seed", &gogit.CommitOptions{Author: sig, Committer: sig}); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if _, err := work.CreateRemote(&gitconfig.RemoteConfig{
		Name: "bare",
		URLs: []string{bareDir},
	}); err != nil {
		t.Fatalf("create remote: %v", err)
	}
	if err := work.Push(&gogit.PushOptions{
		RemoteName: "bare",
		RefSpecs:   []gitconfig.RefSpec{"refs/heads/master:refs/heads/master"},
	}); err != nil {
		t.Fatalf("push: %v", err)
	}

	bare, err := gogit.PlainOpen(bareDir)
	if err != nil {
		t.Fatalf("open bare: %v", err)
	}
	headRef := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("master"))
	if err := bare.Storer.SetReference(headRef); err != nil {
		t.Fatalf("set HEAD: %v", err)
	}

	return NewCodeService(czconfig.GitConfig{ReposRoot: root})
}

func TestCodeService_WalkTree_VisitsAllBlobs(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"main.go":         "package main\n\nfunc main() {}\n", // 30 bytes
		"README.md":       "hello hi\n\n",                     // 10 bytes
		"static/index.js": "console.log('hi');\n\n",           // 20 bytes
	}
	svc := newTestRepoWithFiles(t, "alice", "demo", files)

	seen := make(map[string]int64)
	err := svc.WalkTree(context.Background(), "alice", "demo", "", func(path string, size int64) error {
		if _, dup := seen[path]; dup {
			t.Errorf("path %s visited more than once", path)
		}
		seen[path] = size
		return nil
	})
	if err != nil {
		t.Fatalf("WalkTree: %v", err)
	}

	if len(seen) != len(files) {
		t.Fatalf("expected %d blobs, got %d (%+v)", len(files), len(seen), seen)
	}
	for path, want := range files {
		got, ok := seen[path]
		if !ok {
			t.Errorf("missing path %s", path)
			continue
		}
		if got <= 0 {
			t.Errorf("path %s: expected positive size, got %d", path, got)
		}
		if int(got) != len(want) {
			t.Errorf("path %s: expected size %d, got %d", path, len(want), got)
		}
	}
}

func TestCodeService_WalkTree_AbortOnError(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"a.go": "package a\n",
		"b.go": "package b\n",
		"c.go": "package c\n",
	}
	svc := newTestRepoWithFiles(t, "alice", "abort", files)

	sentinel := errors.New("stop here")
	calls := 0
	err := svc.WalkTree(context.Background(), "alice", "abort", "", func(path string, size int64) error {
		calls++
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 callback before abort, got %d", calls)
	}
}
