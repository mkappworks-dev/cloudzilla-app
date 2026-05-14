package service

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	gitobj "github.com/go-git/go-git/v5/plumbing/object"

	czconfig "github.com/mkappworks-dev/cloudzilla-app/internal/config"
)

// newTestRepoWithCommits initializes a bare repo at <root>/<owner>/<name>.git
// and pushes a chain of commits with the given author times. Returns the
// CodeService configured to read from `root`. Production code only ever sees
// bare repos under ReposRoot, so this mirrors that exactly.
func newTestRepoWithCommits(t *testing.T, owner, name string, times []time.Time) *CodeService {
	t.Helper()
	root := t.TempDir()

	bareDir := filepath.Join(root, owner, name+".git")
	if err := os.MkdirAll(filepath.Dir(bareDir), 0o755); err != nil {
		t.Fatalf("mkdir owner: %v", err)
	}
	if _, err := gogit.PlainInit(bareDir, true); err != nil {
		t.Fatalf("plain init bare: %v", err)
	}

	// Non-bare working repo to author commits in, then push to the bare repo.
	workDir := t.TempDir()
	work, err := gogit.PlainInit(workDir, false)
	if err != nil {
		t.Fatalf("plain init work: %v", err)
	}
	wt, err := work.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}

	for i, when := range times {
		filename := filepath.Join(workDir, "f.txt")
		// Distinct content per commit so the worktree has something to add.
		body := strconv.Itoa(i) + ":" + when.Format(time.RFC3339Nano)
		if err := os.WriteFile(filename, []byte(body), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		if _, err := wt.Add("f.txt"); err != nil {
			t.Fatalf("add: %v", err)
		}
		sig := &gitobj.Signature{Name: "Tester", Email: "tester@example.com", When: when}
		if _, err := wt.Commit("commit "+strconv.Itoa(i), &gogit.CommitOptions{Author: sig, Committer: sig}); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}

	if _, err := work.CreateRemote(&gitconfig.RemoteConfig{
		Name: "bare",
		URLs: []string{bareDir},
	}); err != nil {
		t.Fatalf("create remote: %v", err)
	}
	// go-git defaults to master for PlainInit. Push both naming conventions
	// so resolveRef("HEAD") can find a commit regardless.
	if err := work.Push(&gogit.PushOptions{
		RemoteName: "bare",
		RefSpecs:   []gitconfig.RefSpec{"refs/heads/master:refs/heads/master"},
	}); err != nil {
		t.Fatalf("push: %v", err)
	}

	// Bare repos created via PlainInit have HEAD -> refs/heads/master already,
	// but be explicit so the test doesn't depend on that default.
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

func TestCodeService_LogSince_StopsAtCutoff(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	tm10 := now.AddDate(0, 0, -10)
	tm5 := now.AddDate(0, 0, -5)
	tm1 := now.AddDate(0, 0, -1)

	svc := newTestRepoWithCommits(t, "alice", "demo", []time.Time{tm10, tm5, tm1})

	cutoff := now.AddDate(0, 0, -3)
	commits, err := svc.LogSince(context.Background(), "alice", "demo", "", cutoff)
	if err != nil {
		t.Fatalf("LogSince: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("expected 1 commit at or after cutoff, got %d (%+v)", len(commits), commits)
	}
	// Allow rounding: go-git stores second precision for author When.
	if commits[0].Time.Unix() != tm1.Unix() {
		t.Errorf("expected commit at tm1=%v, got %v", tm1, commits[0].Time)
	}
}

func TestCodeService_LogSince_EmptyRefDefaultsHead(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	tm2 := now.AddDate(0, 0, -2)
	tm1 := now.AddDate(0, 0, -1)

	svc := newTestRepoWithCommits(t, "bob", "demo", []time.Time{tm2, tm1})

	cutoff := now.AddDate(0, 0, -30)
	commits, err := svc.LogSince(context.Background(), "bob", "demo", "", cutoff)
	if err != nil {
		t.Fatalf("LogSince: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}
}

func TestCodeService_LogSince_MissingRepo(t *testing.T) {
	t.Parallel()
	svc := NewCodeService(czconfig.GitConfig{ReposRoot: t.TempDir()})
	_, err := svc.LogSince(context.Background(), "nobody", "ghost", "", time.Now())
	if err == nil {
		t.Fatal("expected error opening missing repo")
	}
}
