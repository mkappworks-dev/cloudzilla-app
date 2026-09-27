package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	gitobj "github.com/go-git/go-git/v5/plumbing/object"

	czconfig "github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// commitSpec describes one commit to author: a set of files to write (path →
// contents) and the author timestamp.
type commitSpec struct {
	files map[string]string
	when  time.Time
	msg   string
}

// newTestRepoWithFileCommits creates a bare repo at <root>/<owner>/<name>.git
// with the given commit history. Each commit writes/overwrites the listed
// files in a non-bare working repo, then pushes refs/heads/master to the bare
// repo. Mirrors newTestRepoWithCommits's pattern.
func newTestRepoWithFileCommits(t *testing.T, owner, name string, commits []commitSpec) *CodeService {
	t.Helper()
	root := t.TempDir()
	writeBareRepo(t, filepath.Join(root, owner, name+".git"), commits)
	return NewCodeService(czconfig.GitConfig{ReposRoot: root})
}

func writeBareRepo(t *testing.T, bareDir string, commits []commitSpec) {
	t.Helper()
	name := strings.TrimSuffix(filepath.Base(bareDir), ".git")
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

	for _, c := range commits {
		for relPath, body := range c.files {
			abs := filepath.Join(workDir, relPath)
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				t.Fatalf("mkdir for %s: %v", relPath, err)
			}
			if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
				t.Fatalf("write %s: %v", relPath, err)
			}
			if _, err := wt.Add(relPath); err != nil {
				t.Fatalf("add %s: %v", relPath, err)
			}
		}
		sig := &gitobj.Signature{Name: "Tester", Email: "tester@example.com", When: c.when}
		msg := c.msg
		if msg == "" {
			msg = "commit " + filepath.Base(name) + "-" + time.Now().Format(time.RFC3339Nano)
		}
		if _, err := wt.Commit(msg, &gogit.CommitOptions{Author: sig, Committer: sig}); err != nil {
			t.Fatalf("commit: %v", err)
		}
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
}

func testRepo(id int64, owner string) *model.Repository {
	return &model.Repository{ID: id, OwnerName: owner, Name: "demo"}
}

func TestListEntriesWithLastCommit(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	t1 := now.Add(-3 * time.Hour)
	t2 := now.Add(-2 * time.Hour)
	t3 := now.Add(-1 * time.Hour)

	svc := newTestRepoWithFileCommits(t, "alice", "demo", []commitSpec{
		{
			when: t1,
			msg:  "add root readme",
			files: map[string]string{
				"README.md": "hello\n",
			},
		},
		{
			when: t2,
			msg:  "add src files",
			files: map[string]string{
				"src/main.go":   "package main\n",
				"src/helper.go": "package main\n",
			},
		},
		{
			when: t3,
			msg:  "update readme",
			files: map[string]string{
				"README.md": "hello world\n",
			},
		},
	})

	entries, err := svc.ListEntriesWithLastCommit(context.Background(), testRepo(1, "alice"), "", "")
	if err != nil {
		t.Fatalf("ListEntriesWithLastCommit: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 root entries (README.md, src), got %d: %+v", len(entries), entries)
	}

	byName := map[string]TreeEntryWithLastCommit{}
	for _, e := range entries {
		byName[e.Name] = e
	}

	readme, ok := byName["README.md"]
	if !ok {
		t.Fatalf("README.md missing from entries: %+v", entries)
	}
	if readme.IsDir {
		t.Errorf("README.md flagged as dir")
	}
	if readme.LastCommit.Message == "" {
		t.Errorf("README.md LastCommit.Message empty")
	}
	if readme.LastCommit.Message != "update readme" {
		t.Errorf("README.md LastCommit.Message = %q, want %q", readme.LastCommit.Message, "update readme")
	}
	if readme.Size == 0 {
		t.Errorf("README.md Size = 0, want > 0")
	}

	src, ok := byName["src"]
	if !ok {
		t.Fatalf("src dir missing from entries: %+v", entries)
	}
	if !src.IsDir {
		t.Errorf("src not flagged as dir")
	}
	if src.LastCommit.Message == "" {
		t.Errorf("src LastCommit.Message empty")
	}
	if src.LastCommit.Message != "add src files" {
		t.Errorf("src LastCommit.Message = %q, want %q", src.LastCommit.Message, "add src files")
	}

	// Sub-directory listing
	subEntries, err := svc.ListEntriesWithLastCommit(context.Background(), testRepo(1, "alice"), "", "src")
	if err != nil {
		t.Fatalf("ListEntriesWithLastCommit(src): %v", err)
	}
	if len(subEntries) != 2 {
		t.Fatalf("expected 2 entries under src, got %d: %+v", len(subEntries), subEntries)
	}
	for _, e := range subEntries {
		if e.LastCommit.Message == "" {
			t.Errorf("entry %s has empty LastCommit.Message", e.Name)
		}
		if e.LastCommit.Author != "Tester" {
			t.Errorf("entry %s author = %q, want %q", e.Name, e.LastCommit.Author, "Tester")
		}
	}
}

func TestListEntriesWithLastCommit_CachedPerCommit(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	svc := newTestRepoWithFileCommits(t, "bob", "demo", []commitSpec{
		{
			when:  now.Add(-time.Hour),
			msg:   "init",
			files: map[string]string{"a.txt": "a\n"},
		},
	})
	repo := testRepo(1, "bob")

	first, err := svc.ListEntriesWithLastCommit(context.Background(), repo, "", "")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(first))
	}
	second, err := svc.ListEntriesWithLastCommit(context.Background(), repo, "", "")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if &second[0] != &first[0] {
		t.Error("an unchanged commit was listed again instead of served from the cache")
	}

	bareDir := svc.repoPath("bob", "demo")
	cloneDir := t.TempDir()
	clone, err := gogit.PlainClone(cloneDir, false, &gogit.CloneOptions{URL: bareDir})
	if err != nil {
		t.Fatalf("clone bare: %v", err)
	}
	cwt, err := clone.Worktree()
	if err != nil {
		t.Fatalf("clone worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cloneDir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatalf("write b.txt: %v", err)
	}
	if _, err := cwt.Add("b.txt"); err != nil {
		t.Fatalf("add b.txt: %v", err)
	}
	sig := &gitobj.Signature{Name: "Tester", Email: "tester@example.com", When: now}
	if _, err := cwt.Commit("add b", &gogit.CommitOptions{Author: sig, Committer: sig}); err != nil {
		t.Fatalf("commit b: %v", err)
	}
	if err := clone.Push(&gogit.PushOptions{
		RefSpecs: []gitconfig.RefSpec{"refs/heads/master:refs/heads/master"},
	}); err != nil {
		t.Fatalf("push b: %v", err)
	}

	third, err := svc.ListEntriesWithLastCommit(context.Background(), repo, "", "")
	if err != nil {
		t.Fatalf("third call: %v", err)
	}
	if len(third) != 2 {
		t.Errorf("after a new commit: got %d entries, want 2", len(third))
	}
}

// Account deletion and transfer free a name that a new repo can take within
// the TTL; it must never be served the previous holder's listing.
func TestListEntriesWithLastCommit_NewRepoUnderTheNameMisses(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	svc := newTestRepoWithFileCommits(t, "victim", "demo", []commitSpec{
		{when: now.Add(-time.Hour), msg: "private plans", files: map[string]string{"plans.txt": "secret\n"}},
	})
	if _, err := svc.ListEntriesWithLastCommit(context.Background(), testRepo(1, "victim"), "", ""); err != nil {
		t.Fatalf("list the old repo: %v", err)
	}

	bareDir := svc.repoPath("victim", "demo")
	if err := os.RemoveAll(bareDir); err != nil {
		t.Fatalf("remove the old repo: %v", err)
	}
	writeBareRepo(t, bareDir, []commitSpec{
		{when: now, msg: "public", files: map[string]string{"public.txt": "hello\n"}},
	})

	got, err := svc.ListEntriesWithLastCommit(context.Background(), testRepo(2, "victim"), "", "")
	if err != nil {
		t.Fatalf("list the new repo: %v", err)
	}
	if len(got) != 1 || got[0].Name != "public.txt" || got[0].LastCommit.Message != "public" {
		t.Errorf("the new repo was listed as %+v", got)
	}
}

func TestListEntriesWithLastCommit_HEADLiteral(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	svc := newTestRepoWithFileCommits(t, "carol", "demo", []commitSpec{
		{
			when:  now.Add(-time.Hour),
			msg:   "init",
			files: map[string]string{"a.txt": "a\n"},
		},
	})

	// Passing the literal "HEAD" should behave like "" (default HEAD), matching LogSince's behavior.
	entries, err := svc.ListEntriesWithLastCommit(context.Background(), testRepo(1, "carol"), "HEAD", "")
	if err != nil {
		t.Fatalf("ListEntriesWithLastCommit(HEAD): %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}
