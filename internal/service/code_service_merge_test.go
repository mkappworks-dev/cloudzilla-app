package service

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	czconfig "github.com/mkappworks-dev/cloudzilla-app/internal/config"
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
	before := filesUnder(t, objects)
	stats, err := svc.PullDiffStats("alice", "stats", "main", "feature")
	if err != nil {
		t.Fatalf("PullDiffStats: %v", err)
	}
	var written []string
	for path := range filesUnder(t, objects) {
		if !before[path] {
			written = append(written, path)
		}
	}
	if len(written) > 0 {
		sort.Strings(written)
		t.Errorf("PullDiffStats wrote into objects/: %s", strings.Join(written, ", "))
	}
	// main.txt landed on main after feature branched off, so it isn't counted.
	if want := (DiffStats{Files: 3, Added: 2, Deleted: 1}); stats != want {
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

func filesUnder(t *testing.T, dir string) map[string]bool {
	t.Helper()
	paths := map[string]bool{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		paths[rel] = true
		return err
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return paths
}

// pullRepo is a bare repo whose branches the tests grow through CodeService's
// own commit, branch and merge methods.
type pullRepo struct {
	svc  *CodeService
	repo *gogit.Repository
}

func newPullRepo(t *testing.T) *pullRepo {
	t.Helper()
	root := t.TempDir()
	repo, err := gogit.PlainInit(filepath.Join(root, "alice", "pulls.git"), true)
	if err != nil {
		t.Fatalf("init bare repo: %v", err)
	}
	return &pullRepo{svc: NewCodeService(czconfig.GitConfig{ReposRoot: root}), repo: repo}
}

func (r *pullRepo) commit(t *testing.T, branch, path, content string) {
	t.Helper()
	if err := r.svc.CommitFile("alice", "pulls", branch, path, []byte(content), tipTestAuthor, "Add "+path); err != nil {
		t.Fatalf("commit %s on %s: %v", path, branch, err)
	}
}

// newDivergedPull is a repo where feature added a line to a.txt after
// branching from main, and main then added b.txt.
func newDivergedPull(t *testing.T) *pullRepo {
	t.Helper()
	r := newPullRepo(t)
	r.commit(t, "main", "a.txt", "one\n")
	if err := r.svc.CreateBranch("alice", "pulls", "feature", "main"); err != nil {
		t.Fatalf("create feature: %v", err)
	}
	r.commit(t, "feature", "a.txt", "one\ntwo\n")
	r.commit(t, "main", "b.txt", "b\n")
	return r
}

// mergeMainIntoFeature brings feature up to date the way a PR author does,
// with a merge commit whose first parent is feature, and returns the main
// commit it merged.
func (r *pullRepo) mergeMainIntoFeature(t *testing.T) plumbing.Hash {
	t.Helper()
	merged := branchTip(t, r.repo, "main")
	if err := r.svc.ThreeWayMergePullRequest("alice", "pulls", "feature", "main", tipTestAuthor); err != nil {
		t.Fatalf("merge main into feature: %v", err)
	}
	return merged
}

func fileSummaries(d *PRDiffResult) []string {
	var out []string
	for _, f := range d.Files {
		path := f.DisplayPath()
		switch {
		case f.IsNew:
			path += " (new)"
		case f.IsDelete:
			path += " (deleted)"
		}
		out = append(out, fmt.Sprintf("%s +%d -%d", path, f.Added, f.Deleted))
	}
	sort.Strings(out)
	return out
}

func TestGetPullDiff_BaseMovedOn(t *testing.T) {
	t.Parallel()
	r := newDivergedPull(t)

	d, err := r.svc.GetPullDiff("alice", "pulls", "main", "feature")
	if err != nil {
		t.Fatalf("GetPullDiff: %v", err)
	}
	if got, want := fileSummaries(d), []string{"a.txt +1 -0"}; !reflect.DeepEqual(got, want) {
		t.Errorf("files = %q, want %q", got, want)
	}
	if d.TotalAdded != 1 || d.TotalDeleted != 0 {
		t.Errorf("totals = +%d -%d, want +1 -0", d.TotalAdded, d.TotalDeleted)
	}
}

func TestGetPullDiff_AfterMergingBaseIntoHead(t *testing.T) {
	t.Parallel()
	r := newDivergedPull(t)
	r.mergeMainIntoFeature(t)
	r.commit(t, "main", "c.txt", "c\n")

	d, err := r.svc.GetPullDiff("alice", "pulls", "main", "feature")
	if err != nil {
		t.Fatalf("GetPullDiff: %v", err)
	}
	if got, want := fileSummaries(d), []string{"a.txt +1 -0"}; !reflect.DeepEqual(got, want) {
		t.Errorf("files = %q, want %q", got, want)
	}
}

func TestGetPullDiff_UnrelatedHistoriesDiffTheTips(t *testing.T) {
	t.Parallel()
	r := newPullRepo(t)
	r.commit(t, "main", "a.txt", "one\n")
	r.commit(t, "orphan", "b.txt", "b\n")

	d, err := r.svc.GetPullDiff("alice", "pulls", "main", "orphan")
	if err != nil {
		t.Fatalf("GetPullDiff: %v", err)
	}
	if got, want := fileSummaries(d), []string{"a.txt (deleted) +0 -1", "b.txt (new) +1 -0"}; !reflect.DeepEqual(got, want) {
		t.Errorf("files = %q, want %q", got, want)
	}
}
