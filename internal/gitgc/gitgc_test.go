package gitgc_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage"

	"github.com/mkappworks-dev/cloudzilla-app/internal/gitgc"
)

func TestPrune(t *testing.T) {
	repoPath := t.TempDir()
	repo, err := gogit.PlainInit(repoPath, true)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	st := repo.Storer

	// Reachable chain: blob → tree → commit, anchored by a branch ref.
	blob := writeObject(t, st, plumbing.BlobObject, []byte("reachable blob\n"))
	tree := writeTree(t, st, "file.txt", blob)
	commit := writeCommit(t, st, tree)
	if err := st.SetReference(plumbing.NewHashReference("refs/heads/main", commit)); err != nil {
		t.Fatalf("SetReference: %v", err)
	}

	oldOrphan := writeObject(t, st, plumbing.BlobObject, []byte("old orphan\n"))
	youngOrphan := writeObject(t, st, plumbing.BlobObject, []byte("young orphan\n"))

	// Age the whole reachable chain and the old orphan past the grace
	// period; only the young orphan stays recent. This proves the chain
	// is kept by reachability, not by age.
	old := time.Now().Add(-30 * 24 * time.Hour)
	for _, h := range []plumbing.Hash{blob, tree, commit, oldOrphan} {
		ageObject(t, repoPath, h, old)
	}

	res, err := gitgc.Prune(repoPath, gitgc.Options{Grace: 14 * 24 * time.Hour})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}

	if res.Scanned != 5 {
		t.Errorf("Scanned: want 5, got %d", res.Scanned)
	}
	if res.Pruned != 1 {
		t.Errorf("Pruned: want 1 (old orphan), got %d", res.Pruned)
	}
	if res.Kept != 1 {
		t.Errorf("Kept: want 1 (young orphan), got %d", res.Kept)
	}

	assertAbsent(t, repoPath, oldOrphan)
	assertPresent(t, repoPath, youngOrphan)
	for _, h := range []plumbing.Hash{blob, tree, commit} {
		assertPresent(t, repoPath, h)
	}
}

func TestPrune_DryRunDeletesNothing(t *testing.T) {
	repoPath := t.TempDir()
	repo, err := gogit.PlainInit(repoPath, true)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	st := repo.Storer

	commit := writeCommit(t, st, writeTree(t, st, "f", writeObject(t, st, plumbing.BlobObject, []byte("x\n"))))
	if err := st.SetReference(plumbing.NewHashReference("refs/heads/main", commit)); err != nil {
		t.Fatalf("SetReference: %v", err)
	}
	orphan := writeObject(t, st, plumbing.BlobObject, []byte("orphan\n"))
	ageObject(t, repoPath, orphan, time.Now().Add(-30*24*time.Hour))

	res, err := gitgc.Prune(repoPath, gitgc.Options{Grace: 14 * 24 * time.Hour, DryRun: true})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if res.Pruned != 1 {
		t.Errorf("Pruned: want 1, got %d", res.Pruned)
	}
	assertPresent(t, repoPath, orphan)
}

func TestPrune_SkipsRepoWithNoRefs(t *testing.T) {
	repoPath := t.TempDir()
	repo, err := gogit.PlainInit(repoPath, true)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	orphan := writeObject(t, repo.Storer, plumbing.BlobObject, []byte("orphan\n"))
	ageObject(t, repoPath, orphan, time.Now().Add(-30*24*time.Hour))

	res, err := gitgc.Prune(repoPath, gitgc.Options{Grace: 14 * 24 * time.Hour})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if !res.Skipped {
		t.Error("Skipped: want true for a repo with no refs")
	}
	if res.Pruned != 0 {
		t.Errorf("Pruned: want 0 for a skipped repo, got %d", res.Pruned)
	}
	assertPresent(t, repoPath, orphan)
}

func TestPrune_RejectsSymlinkedObjectsDir(t *testing.T) {
	repoPath := t.TempDir()
	repo, err := gogit.PlainInit(repoPath, true)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	st := repo.Storer
	commit := writeCommit(t, st, writeTree(t, st, "f", writeObject(t, st, plumbing.BlobObject, []byte("x\n"))))
	if err := st.SetReference(plumbing.NewHashReference("refs/heads/main", commit)); err != nil {
		t.Fatalf("SetReference: %v", err)
	}

	// Relative, in-repo symlink: go-git's chroot resolves it (so the
	// reachability walk still succeeds), leaving locateObjectsDir as the
	// guard that must reject it.
	objects := filepath.Join(repoPath, "objects")
	if err := os.Rename(objects, filepath.Join(repoPath, "objects-real")); err != nil {
		t.Fatalf("rename objects: %v", err)
	}
	if err := os.Symlink("objects-real", objects); err != nil {
		t.Fatalf("symlink objects: %v", err)
	}

	_, err = gitgc.Prune(repoPath, gitgc.Options{Grace: 0})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected a symlink rejection error, got %v", err)
	}
}

func writeObject(t *testing.T, st storage.Storer, typ plumbing.ObjectType, content []byte) plumbing.Hash {
	t.Helper()
	o := &plumbing.MemoryObject{}
	o.SetType(typ)
	if _, err := o.Write(content); err != nil {
		t.Fatalf("write object: %v", err)
	}
	h, err := st.SetEncodedObject(o)
	if err != nil {
		t.Fatalf("store object: %v", err)
	}
	return h
}

func writeTree(t *testing.T, st storage.Storer, name string, blob plumbing.Hash) plumbing.Hash {
	t.Helper()
	tree := &object.Tree{
		Entries: []object.TreeEntry{{Name: name, Mode: filemode.Regular, Hash: blob}},
	}
	o := &plumbing.MemoryObject{}
	if err := tree.Encode(o); err != nil {
		t.Fatalf("encode tree: %v", err)
	}
	h, err := st.SetEncodedObject(o)
	if err != nil {
		t.Fatalf("store tree: %v", err)
	}
	return h
}

func writeCommit(t *testing.T, st storage.Storer, tree plumbing.Hash) plumbing.Hash {
	t.Helper()
	sig := object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()}
	commit := &object.Commit{Author: sig, Committer: sig, Message: "test commit", TreeHash: tree}
	o := &plumbing.MemoryObject{}
	if err := commit.Encode(o); err != nil {
		t.Fatalf("encode commit: %v", err)
	}
	h, err := st.SetEncodedObject(o)
	if err != nil {
		t.Fatalf("store commit: %v", err)
	}
	return h
}

func looseObjectPath(repoPath string, h plumbing.Hash) string {
	s := h.String()
	return filepath.Join(repoPath, "objects", s[:2], s[2:])
}

func ageObject(t *testing.T, repoPath string, h plumbing.Hash, when time.Time) {
	t.Helper()
	if err := os.Chtimes(looseObjectPath(repoPath, h), when, when); err != nil {
		t.Fatalf("chtimes %s: %v", h, err)
	}
}

func assertPresent(t *testing.T, repoPath string, h plumbing.Hash) {
	t.Helper()
	if _, err := os.Stat(looseObjectPath(repoPath, h)); err != nil {
		t.Errorf("expected object %s present: %v", h, err)
	}
}

func assertAbsent(t *testing.T, repoPath string, h plumbing.Hash) {
	t.Helper()
	if _, err := os.Stat(looseObjectPath(repoPath, h)); !os.IsNotExist(err) {
		t.Errorf("expected object %s pruned, stat err = %v", h, err)
	}
}
