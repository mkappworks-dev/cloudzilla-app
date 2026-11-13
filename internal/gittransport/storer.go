// Package gittransport contains transport-layer adapters around go-git's
// receive-pack path: a server whose ref updates are connectivity-checked,
// vetted compare-and-swaps, a storer wrapper that forces correct thin-pack
// handling, an incoming-pack size limiter, and a byte counter for
// observability. See docs/git-transport.md → "Thin packs", "Connectivity", and
// "Concurrent ref updates".
package gittransport

import (
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

// receivePackStorer hides any methods of the wrapped storer that aren't
// part of storer.Storer itself. Critically it hides PackfileWriter(),
// which is what triggers the broken fast path inside
// packfile.UpdateObjectStorage.
//
// Interface-embedding (not struct-embedding) is load-bearing here:
// struct-embedding *filesystem.Storage would re-promote PackfileWriter
// and defeat the wrapper.
type receivePackStorer struct {
	storer.Storer
}

// WrapForReceive returns a view of s suitable for go-git's
// NewReceivePackSession that forces the slow-but-correct
// parse-with-storage pack-ingestion path.
func WrapForReceive(s storer.Storer) storer.Storer {
	return receivePackStorer{s}
}

// hidePullRefs leaves refs/pull/* out of the ref advertisement; a fork pull request's head is for the server, not for
// clones. Only IterReferences is filtered, so a ref is still readable and writable by name.
type hidePullRefs struct {
	storer.Storer
}

// HidePullRefs returns a view of s whose IterReferences omits refs/pull/*.
func HidePullRefs(s storer.Storer) storer.Storer {
	return hidePullRefs{s}
}

func (h hidePullRefs) IterReferences() (storer.ReferenceIter, error) {
	it, err := h.Storer.IterReferences()
	if err != nil {
		return nil, err
	}
	return storer.NewReferenceFilteredIter(func(r *plumbing.Reference) bool {
		return !strings.HasPrefix(r.Name().String(), "refs/pull/")
	}, it), nil
}
