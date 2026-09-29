package testutil

import (
	"io"
	"os"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5/osfs"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

// WriteCommit stores a commit of an empty tree with the given parents.
func WriteCommit(t *testing.T, st storer.EncodedObjectStorer, msg string, parents ...plumbing.Hash) plumbing.Hash {
	t.Helper()
	tree := writeObject(t, st, &object.Tree{})
	sig := object.Signature{Name: "Tester", Email: "tester@example.com", When: time.Unix(0, 0).UTC()}
	return writeObject(t, st, &object.Commit{Author: sig, Committer: sig, Message: msg, TreeHash: tree, ParentHashes: parents})
}

func writeObject(t *testing.T, st storer.EncodedObjectStorer, o object.Object) plumbing.Hash {
	t.Helper()
	obj := st.NewEncodedObject()
	if err := o.Encode(obj); err != nil {
		t.Fatalf("encode object: %v", err)
	}
	h, err := st.SetEncodedObject(obj)
	if err != nil {
		t.Fatalf("store object: %v", err)
	}
	return h
}

// PushDuringCommit lands a push on ref after commit has read the branch but
// before it moves it. It holds go-git's own lock on the loose ref file while
// commit runs, waits for commit to write a commit on top of from, then moves
// ref to to (the "pushed" commit, itself a child of from) and lets commit finish.
func PushDuringCommit(t *testing.T, gitDir string, ref plumbing.ReferenceName, from, to plumbing.Hash, commit func()) {
	t.Helper()
	f, err := osfs.New(gitDir).OpenFile(ref.String(), os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open %s: %v", ref, err)
	}
	defer func() { _ = f.Close() }()
	if err := f.Lock(); err != nil {
		t.Fatalf("lock %s: %v", ref, err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		commit()
	}()
	if !awaitChildCommit(gitDir, from, to, done) {
		t.Fatalf("commit wrote no commit on top of %s before finishing", from)
	}

	// Same length as the old hash, so an unlocked reader never sees a short file.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("seek %s: %v", ref, err)
	}
	if _, err := f.Write([]byte(to.String() + "\n")); err != nil {
		t.Fatalf("move %s: %v", ref, err)
	}
	if err := f.Unlock(); err != nil {
		t.Fatalf("unlock %s: %v", ref, err)
	}
	<-done
}

// awaitChildCommit reports whether a commit other than exclude, with parent as
// its first parent, appears while commit is still running.
func awaitChildCommit(gitDir string, parent, exclude plumbing.Hash, done <-chan struct{}) bool {
	deadline := time.After(10 * time.Second)
	for {
		if hasChildCommit(gitDir, parent, exclude) {
			return true
		}
		select {
		case <-done:
			return false
		case <-deadline:
			return false
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func hasChildCommit(gitDir string, parent, exclude plumbing.Hash) bool {
	repo, err := gogit.PlainOpen(gitDir)
	if err != nil {
		return false
	}
	iter, err := repo.CommitObjects()
	if err != nil {
		return false
	}
	found := false
	_ = iter.ForEach(func(c *object.Commit) error {
		if c.Hash != exclude && len(c.ParentHashes) > 0 && c.ParentHashes[0] == parent {
			found = true
			return storer.ErrStop
		}
		return nil
	})
	return found
}
