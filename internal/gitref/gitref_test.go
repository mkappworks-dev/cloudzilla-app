package gitref_test

import (
	"errors"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/gitref"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

var (
	mainRef  = plumbing.NewBranchReferenceName("main")
	freshRef = plumbing.NewBranchReferenceName("fresh")
)

// refRepo is a bare repo with main at base. next and other are commits on base
// that no ref points at.
type refRepo struct {
	repo              *gogit.Repository
	base, next, other plumbing.Hash
}

func newRefRepo(t *testing.T) *refRepo {
	t.Helper()
	repo, err := gogit.PlainInit(t.TempDir(), true)
	if err != nil {
		t.Fatalf("init bare repo: %v", err)
	}
	r := &refRepo{repo: repo}
	r.base = testutil.WriteCommit(t, repo.Storer, "base")
	r.next = testutil.WriteCommit(t, repo.Storer, "next", r.base)
	r.other = testutil.WriteCommit(t, repo.Storer, "other", r.base)
	r.set(t, mainRef, r.base)
	return r
}

func (r *refRepo) set(t *testing.T, name plumbing.ReferenceName, h plumbing.Hash) {
	t.Helper()
	if err := r.repo.Storer.SetReference(plumbing.NewHashReference(name, h)); err != nil {
		t.Fatalf("set %s: %v", name, err)
	}
}

func (r *refRepo) get(t *testing.T, name plumbing.ReferenceName) plumbing.Hash {
	t.Helper()
	ref, err := r.repo.Storer.Reference(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return ref.Hash()
}

func (r *refRepo) assertRefsListable(t *testing.T) {
	t.Helper()
	refs, err := r.repo.References()
	if err != nil {
		t.Fatalf("list refs: %v", err)
	}
	if err := refs.ForEach(func(*plumbing.Reference) error { return nil }); err != nil {
		t.Errorf("list refs: %v", err)
	}
}

func TestMove_UpdatesCreatesAndDeletes(t *testing.T) {
	r := newRefRepo(t)

	if err := gitref.Move(r.repo.Storer, mainRef, r.base, r.next); err != nil {
		t.Fatalf("update main: %v", err)
	}
	if got := r.get(t, mainRef); got != r.next {
		t.Errorf("main = %s, want %s", got, r.next)
	}

	if err := gitref.Move(r.repo.Storer, freshRef, plumbing.ZeroHash, r.base); err != nil {
		t.Fatalf("create fresh: %v", err)
	}
	if got := r.get(t, freshRef); got != r.base {
		t.Errorf("fresh = %s, want %s", got, r.base)
	}

	if err := gitref.Move(r.repo.Storer, mainRef, r.next, plumbing.ZeroHash); err != nil {
		t.Fatalf("delete main: %v", err)
	}
	if _, err := r.repo.Storer.Reference(mainRef); !errors.Is(err, plumbing.ErrReferenceNotFound) {
		t.Errorf("read deleted main: err = %v, want ErrReferenceNotFound", err)
	}
	r.assertRefsListable(t)
}

// A ref that exists only in packed-refs has no loose file to compare against.
func TestMove_PackedRef(t *testing.T) {
	r := newRefRepo(t)
	if err := r.repo.Storer.PackRefs(); err != nil {
		t.Fatalf("pack refs: %v", err)
	}

	if err := gitref.Move(r.repo.Storer, mainRef, r.other, r.next); !errors.Is(err, gitref.ErrMoved) {
		t.Errorf("stale update: err = %v, want ErrMoved", err)
	}
	if err := gitref.Move(r.repo.Storer, mainRef, r.base, r.next); err != nil {
		t.Fatalf("update main: %v", err)
	}
	if got := r.get(t, mainRef); got != r.next {
		t.Errorf("main = %s, want %s", got, r.next)
	}
	r.assertRefsListable(t)
}

func TestMove_RefMovedSinceRead(t *testing.T) {
	for _, tc := range []struct {
		name string
		to   func(*refRepo) plumbing.Hash
	}{
		{"update", func(r *refRepo) plumbing.Hash { return r.next }},
		{"delete", func(*refRepo) plumbing.Hash { return plumbing.ZeroHash }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRefRepo(t)
			r.set(t, mainRef, r.other)

			err := gitref.Move(r.repo.Storer, mainRef, r.base, tc.to(r))
			if !errors.Is(err, gitref.ErrMoved) {
				t.Errorf("err = %v, want ErrMoved", err)
			}
			if got := r.get(t, mainRef); got != r.other {
				t.Errorf("main = %s, want %s", got, r.other)
			}
		})
	}
}

func TestMove_RefCreatedSinceRead(t *testing.T) {
	r := newRefRepo(t)
	r.set(t, freshRef, r.other)

	err := gitref.Move(r.repo.Storer, freshRef, plumbing.ZeroHash, r.next)
	if !errors.Is(err, gitref.ErrMoved) {
		t.Errorf("err = %v, want ErrMoved", err)
	}
	if got := r.get(t, freshRef); got != r.other {
		t.Errorf("fresh = %s, want %s", got, r.other)
	}
}

// go-git's CheckAndSetReference leaves an empty loose ref file behind when the
// ref is gone, and every ref listing then fails, so clone and fetch break.
func TestMove_RefDeletedSinceRead(t *testing.T) {
	r := newRefRepo(t)
	if err := r.repo.Storer.RemoveReference(mainRef); err != nil {
		t.Fatalf("delete main: %v", err)
	}

	if err := gitref.Move(r.repo.Storer, mainRef, r.base, r.next); !errors.Is(err, gitref.ErrMoved) {
		t.Errorf("err = %v, want ErrMoved", err)
	}
	r.assertRefsListable(t)
}
