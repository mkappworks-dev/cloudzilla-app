// Package gittransport contains transport-layer adapters around go-git's
// receive-pack implementation.
//
// Today it exists for one purpose: routing the receive-pack path off
// go-git's filesystem fast path, which cannot resolve REF_DELTA references
// whose base lives outside the incoming thin pack. See
// docs/git-transport.md → "Thin packs" and
// docs/superpowers/specs/2026-05-15-git-receive-thin-pack-fix-design.md.
package gittransport

import "github.com/go-git/go-git/v5/plumbing/storer"

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
