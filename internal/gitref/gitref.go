// Package gitref moves refs with compare-and-swap, so a writer can't overwrite
// a ref update it never saw.
package gitref

import (
	"errors"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage"
)

// ErrMoved means a ref no longer held the value an update was computed from,
// so the update was not applied.
var ErrMoved = errors.New("ref changed since it was read")

// Move moves name from one hash to another, creating it when from is zero and
// deleting it when to is zero, and fails with ErrMoved if the ref doesn't hold
// from.
func Move(st storer.ReferenceStorer, name plumbing.ReferenceName, from, to plumbing.Hash) error {
	// go-git can't require a ref to be absent or delete one conditionally, and
	// CheckAndSetReference on a deleted ref leaves an empty loose file that
	// breaks every ref listing; checking first narrows these gaps to the
	// instant before the write.
	cur, err := st.Reference(name)
	switch {
	case errors.Is(err, plumbing.ErrReferenceNotFound):
		if !from.IsZero() {
			return ErrMoved
		}
	case err != nil:
		return err
	case cur.Hash() != from:
		return ErrMoved
	}

	if to.IsZero() {
		return st.RemoveReference(name)
	}
	var old *plumbing.Reference
	if !from.IsZero() {
		old = plumbing.NewHashReference(name, from)
	}
	err = st.CheckAndSetReference(plumbing.NewHashReference(name, to), old)
	if errors.Is(err, storage.ErrReferenceHasChanged) || errors.Is(err, plumbing.ErrReferenceNotFound) {
		return ErrMoved
	}
	return err
}
