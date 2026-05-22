package gittransport_test

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1" //nolint:gosec // the git packfile trailer is defined as SHA-1
	"encoding/binary"
	"io"
	"testing"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage/filesystem"

	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
)

// TestWrapForReceive_ResolvesThinPackRefDelta reproduces the actual
// thin-pack bug: a packfile whose only object is a REF_DELTA against a
// base that lives in storage but not in the pack. The unwrapped fast
// path cannot resolve it; the wrapped parse-with-storage path can. This
// is the regression guard the manual-only verification used to be.
func TestWrapForReceive_ResolvesThinPackRefDelta(t *testing.T) {
	baseContent := []byte("thin-pack base object\n")
	target := []byte("thin-pack resolved target object\n")
	wantTarget := plumbing.ComputeHash(plumbing.BlobObject, target)

	newStorage := func() *filesystem.Storage {
		return filesystem.NewStorage(memfs.New(), cache.NewObjectLRUDefault())
	}

	t.Run("unwrapped storer cannot resolve external base", func(t *testing.T) {
		st := newStorage()
		baseHash := seedBlob(t, st, baseContent)
		pack := buildThinRefDeltaPack(t, baseHash, len(baseContent), target)

		if err := packfile.UpdateObjectStorage(st, bytes.NewReader(pack)); err == nil {
			t.Fatal("expected the unwrapped fast path to fail on an external REF_DELTA base; " +
				"if go-git fixed this, WrapForReceive may no longer be needed")
		}
	})

	t.Run("wrapped storer resolves the thin pack", func(t *testing.T) {
		st := newStorage()
		baseHash := seedBlob(t, st, baseContent)
		pack := buildThinRefDeltaPack(t, baseHash, len(baseContent), target)

		if err := packfile.UpdateObjectStorage(gittransport.WrapForReceive(st), bytes.NewReader(pack)); err != nil {
			t.Fatalf("wrapped storer failed to resolve thin pack: %v", err)
		}

		obj, err := st.EncodedObject(plumbing.BlobObject, wantTarget)
		if err != nil {
			t.Fatalf("resolved target %s not found in storage: %v", wantTarget, err)
		}
		r, err := obj.Reader()
		if err != nil {
			t.Fatalf("open resolved object: %v", err)
		}
		defer r.Close()
		got, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("read resolved object: %v", err)
		}
		if !bytes.Equal(got, target) {
			t.Fatalf("resolved content mismatch:\n want %q\n  got %q", target, got)
		}
	})
}

func seedBlob(t *testing.T, st storer.Storer, content []byte) plumbing.Hash {
	t.Helper()
	obj := &plumbing.MemoryObject{}
	obj.SetType(plumbing.BlobObject)
	if _, err := obj.Write(content); err != nil {
		t.Fatalf("write blob content: %v", err)
	}
	h, err := st.SetEncodedObject(obj)
	if err != nil {
		t.Fatalf("seed base blob: %v", err)
	}
	return h
}

// buildThinRefDeltaPack assembles a minimal v2 packfile holding a single
// OBJ_REF_DELTA whose base is identified by hash but not present in the
// pack — a genuine thin pack. The delta is pure-insert, so resolving it
// only needs the base for source-size validation.
func buildThinRefDeltaPack(t *testing.T, baseHash plumbing.Hash, baseSize int, target []byte) []byte {
	t.Helper()
	if len(target) > 127 {
		t.Fatalf("test helper supports targets up to 127 bytes, got %d", len(target))
	}

	var delta bytes.Buffer
	delta.Write(deltaVarint(baseSize))
	delta.Write(deltaVarint(len(target)))
	delta.WriteByte(byte(len(target))) // insert instruction
	delta.Write(target)

	var pack bytes.Buffer
	pack.WriteString("PACK")
	_ = binary.Write(&pack, binary.BigEndian, uint32(2)) // pack version
	_ = binary.Write(&pack, binary.BigEndian, uint32(1)) // object count

	pack.Write(packObjectHeader(7, delta.Len())) // 7 = OBJ_REF_DELTA
	pack.Write(baseHash[:])

	var zb bytes.Buffer
	zw := zlib.NewWriter(&zb)
	if _, err := zw.Write(delta.Bytes()); err != nil {
		t.Fatalf("zlib write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}
	pack.Write(zb.Bytes())

	sum := sha1.Sum(pack.Bytes()) //nolint:gosec // git packfile trailer is SHA-1
	pack.Write(sum[:])
	return pack.Bytes()
}

// packObjectHeader encodes the variable-length type+size header that
// precedes every object in a packfile.
func packObjectHeader(objType byte, size int) []byte {
	c := objType<<4 | byte(size&0x0F)
	size >>= 4
	var out []byte
	for size > 0 {
		out = append(out, c|0x80)
		c = byte(size & 0x7F)
		size >>= 7
	}
	return append(out, c)
}

// deltaVarint encodes a little-endian base-128 size, as used for the
// source and target sizes in a delta header.
func deltaVarint(n int) []byte {
	var out []byte
	for {
		c := byte(n & 0x7F)
		n >>= 7
		if n == 0 {
			return append(out, c)
		}
		out = append(out, c|0x80)
	}
}
