package service

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	czconfig "github.com/mkappworks-dev/cloudzilla-app/internal/config"
)

func TestPullCommits_Linear(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	times := []time.Time{
		now.Add(-4 * time.Hour),
		now.Add(-3 * time.Hour),
		now.Add(-2 * time.Hour),
		now.Add(-1 * time.Hour),
	}
	svc := newTestRepoWithCommits(t, "alice", "linear", times)

	log, err := svc.GetCommits("alice", "linear", "master", 1, 10)
	if err != nil {
		t.Fatalf("GetCommits: %v", err)
	}
	if len(log.Commits) != 4 {
		t.Fatalf("expected 4 commits, got %d", len(log.Commits))
	}
	// Commits are in reverse-chron order: [3,2,1,0].
	// Use full hash of commit 0 (oldest) as base, HEAD as head.
	oldestSHA := log.Commits[3].FullHash
	headSHA := log.Commits[0].FullHash

	commits, err := svc.PullCommits("alice", "linear", oldestSHA, headSHA)
	if err != nil {
		t.Fatalf("PullCommits: %v", err)
	}
	// head has 3 commits on top of base (commits 1, 2, 3).
	if len(commits) != 3 {
		t.Errorf("expected 3 commits, got %d", len(commits))
	}
	for _, c := range commits {
		if c.Hash == "" || c.FullHash == "" || c.Author == "" {
			t.Errorf("commit has empty fields: %+v", c)
		}
	}
}

func TestPullCommits_Divergent(t *testing.T) {
	t.Parallel()
	svc, work, workDir, bareDir := mergeabilityTestRepo(t, "bob", "diverge")
	_ = bareDir
	renameDefaultToMain(t, work)

	now := time.Now().UTC()

	// shared base commit on main
	commitFile(t, work, workDir, "base.txt", "base\n", "base: init")
	pushBranch(t, work, "main")

	// feature branch: 2 unique commits
	createBranch(t, work, "feature")
	commitFile(t, work, workDir, "feature.txt", "f1\n", "feature: c1")
	commitFile(t, work, workDir, "feature.txt", "f2\n", "feature: c2")
	pushBranch(t, work, "feature")

	// main advances: 1 unique commit
	checkout(t, work, "main")
	commitFile(t, work, workDir, "main.txt", "m\n", "main: advance")
	pushBranch(t, work, "main")
	_ = now

	commits, err := svc.PullCommits("bob", "diverge", "main", "feature")
	if err != nil {
		t.Fatalf("PullCommits: %v", err)
	}
	if len(commits) != 2 {
		t.Errorf("expected 2 commits (feature-only), got %d", len(commits))
	}
}

func TestPullCommits_NoOp(t *testing.T) {
	t.Parallel()
	svc := newTestRepoWithCommits(t, "carol", "noop", []time.Time{
		time.Now().Add(-1 * time.Hour),
		time.Now(),
	})

	// base == head: both point to master HEAD
	commits, err := svc.PullCommits("carol", "noop", "master", "master")
	if err != nil {
		t.Fatalf("PullCommits: %v", err)
	}
	if len(commits) != 0 {
		t.Errorf("expected 0 commits (base==head), got %d", len(commits))
	}
}

// Commits from one second list as `git rev-list main..feature` lists this
// history: in the order they were queued, a merge's parents in turn.
func TestPullCommits_SameSecondInRevListOrder(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	h := newPushHistory(t, filepath.Join(root, "alice", "scripted.git"), "tester@example.com", 0)
	fork := h.line(2)
	x := h.commit("X", fork)
	y := h.commit("Y", fork)
	m1 := h.commit("Merge Y", x, y)
	w := h.commit("W", fork)
	m2 := h.commit("Merge W", m1, w)
	setBranch(t, h.repo, "main", fork)
	setBranch(t, h.repo, "feature", m2)

	commits, err := NewCodeService(czconfig.GitConfig{ReposRoot: root}).PullCommits("alice", "scripted", "main", "feature")
	if err != nil {
		t.Fatalf("PullCommits: %v", err)
	}
	var got []string
	for _, c := range commits {
		got = append(got, c.Message)
	}
	if want := []string{"Merge W", "Merge Y", "W", "X", "Y"}; !slices.Equal(got, want) {
		t.Errorf("PullCommits = %v, want %v", got, want)
	}
}

// PullCommits lists head's own commits newest first, without the base commits
// head merged. The branches sit on history whose commits are missing 20 below
// the fork, so a walk through all of either branch's history fails.
func TestPullCommits_ReadsOnlyTheHistorySinceTheFork(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	h := newPushHistory(t, filepath.Join(root, "alice", "shallow.git"), "tester@example.com", time.Minute)
	fork := h.shallowLine(20)
	first := h.commit("Feature 1", fork)
	merged := h.commit("Main change 1", fork)
	second := h.commit("Feature 2", first)
	merge := h.commit("Merge main", second, merged)
	setBranch(t, h.repo, "feature", merge)
	setBranch(t, h.repo, "main", h.commit("Main change 2", merged))

	commits, err := NewCodeService(czconfig.GitConfig{ReposRoot: root}).PullCommits("alice", "shallow", "main", "feature")
	if err != nil {
		t.Fatalf("PullCommits: %v", err)
	}
	var got []string
	for _, c := range commits {
		got = append(got, c.FullHash)
	}
	if want := []string{merge.String(), second.String(), first.String()}; !slices.Equal(got, want) {
		t.Errorf("PullCommits = %v, want %v", got, want)
	}
}
