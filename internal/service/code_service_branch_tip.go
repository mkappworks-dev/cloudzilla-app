package service

import (
	"errors"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage"
)

// ErrRefMoved means a branch moved between reading its tip and advancing it,
// so the new commit was not applied. Handlers answer 409.
var ErrRefMoved = errors.New("branch changed since it was read")

// setBranchTip moves name from oldTip to newTip, or creates it when oldTip is
// zero, failing with ErrRefMoved if anything else moved it first. Receive-pack
// enforces branch protection, so an unconditional write here would be an
// unchecked force push.
func setBranchTip(st storer.ReferenceStorer, name plumbing.ReferenceName, oldTip, newTip plumbing.Hash) error {
	// CheckAndSetReference can't require a ref to be absent, and on a deleted
	// ref it leaves an empty loose file that breaks every ref listing; checking
	// first narrows both gaps to the instant before the write.
	cur, err := st.Reference(name)
	switch {
	case errors.Is(err, plumbing.ErrReferenceNotFound):
		if !oldTip.IsZero() {
			return ErrRefMoved
		}
	case err != nil:
		return err
	case cur.Hash() != oldTip:
		return ErrRefMoved
	}

	var old *plumbing.Reference
	if !oldTip.IsZero() {
		old = plumbing.NewHashReference(name, oldTip)
	}
	err = st.CheckAndSetReference(plumbing.NewHashReference(name, newTip), old)
	if errors.Is(err, storage.ErrReferenceHasChanged) || errors.Is(err, plumbing.ErrReferenceNotFound) {
		return ErrRefMoved
	}
	return err
}
