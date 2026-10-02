package service

import (
	"io/fs"
	"path/filepath"
	"testing"
)

func TestPullDiffStats_MatchesGetPullDiff(t *testing.T) {
	t.Parallel()
	svc, work, workDir, bareDir := mergeabilityTestRepo(t, "alice", "stats")
	renameDefaultToMain(t, work)

	commitFile(t, work, workDir, "edit.txt", "a\nb\nc\n", "init")
	createBranch(t, work, "feature")
	commitFile(t, work, workDir, "edit.txt", "a\nB\nc\nd", "feature: edit, drop trailing newline")
	commitFile(t, work, workDir, "empty.txt", "", "feature: add empty file")
	commitFile(t, work, workDir, "blob.bin", "\x00\x01", "feature: add binary file")
	checkout(t, work, "main")
	commitFile(t, work, workDir, "main.txt", "m\n", "main: diverge")
	pushBranch(t, work, "main")
	pushBranch(t, work, "feature")

	objects := filepath.Join(bareDir, "objects")
	before := countFiles(t, objects)
	stats, err := svc.PullDiffStats("alice", "stats", "main", "feature")
	if err != nil {
		t.Fatalf("PullDiffStats: %v", err)
	}
	if after := countFiles(t, objects); after != before {
		t.Errorf("PullDiffStats wrote %d objects into the repo", after-before)
	}
	// The diff runs between the branch tips, so main.txt counts as deleted.
	if want := (DiffStats{Files: 4, Added: 2, Deleted: 2}); stats != want {
		t.Errorf("PullDiffStats = %+v, want %+v", stats, want)
	}

	diff, err := svc.GetPullDiff("alice", "stats", "main", "feature")
	if err != nil {
		t.Fatalf("GetPullDiff: %v", err)
	}
	if got := (DiffStats{Files: len(diff.Files), Added: diff.TotalAdded, Deleted: diff.TotalDeleted}); got != stats {
		t.Errorf("GetPullDiff totals = %+v, PullDiffStats = %+v", got, stats)
	}
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return err
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return n
}
