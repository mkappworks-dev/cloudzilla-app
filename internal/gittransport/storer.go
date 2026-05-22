// Package gittransport contains transport-layer adapters around go-git's
// receive-pack path: a storer wrapper that forces correct thin-pack
// handling, an incoming-pack size limiter, and a byte counter for
// observability. See docs/git-transport.md → "Thin packs".
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
