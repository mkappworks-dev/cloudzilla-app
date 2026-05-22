package gittransport_test

import (
	"testing"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage/filesystem"

	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
)

// TestWrapForReceive_HidesPackfileWriter is the load-bearing invariant
// for this package. If the wrapped value ever satisfies PackfileWriter,
// go-git's UpdateObjectStorage will take its broken fast path and thin
// packs will fail again. See
// docs/superpowers/specs/2026-05-15-git-receive-thin-pack-fix-design.md.
func TestWrapForReceive_HidesPackfileWriter(t *testing.T) {
	underlying := filesystem.NewStorage(memfs.New(), cache.NewObjectLRUDefault())

	// Precondition: filesystem.Storage MUST implement PackfileWriter,
	// otherwise the wrapping is unnecessary and this whole package
	// should be reconsidered.
	if _, ok := any(underlying).(storer.PackfileWriter); !ok {
		t.Fatal("precondition violated: filesystem.Storage no longer implements storer.PackfileWriter")
	}

	wrapped := gittransport.WrapForReceive(underlying)

	if _, ok := wrapped.(storer.PackfileWriter); ok {
		t.Fatal("wrapped storer must NOT satisfy storer.PackfileWriter; interface-embedding wrapper is broken")
	}
}
