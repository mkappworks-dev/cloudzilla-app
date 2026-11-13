package testutil

import (
	"bytes"
	"io"
	"path"
	"sort"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/storage/memory"
)

// PushClient is a git client for tests: it commits in a scratch store that holds the remote's objects, and encodes
// the receive-pack request that pushes what it made, pack included.
type PushClient struct {
	t       *testing.T
	st      *memory.Storage
	remote  *gogit.Repository
	created []plumbing.Hash
	clock   int64
}

func NewPushClient(t *testing.T, remote *gogit.Repository) *PushClient {
	t.Helper()
	c := &PushClient{t: t, st: memory.NewStorage(), remote: remote}
	iter, err := remote.Storer.IterEncodedObjects(plumbing.AnyObject)
	if err != nil {
		t.Fatalf("list remote objects: %v", err)
	}
	err = iter.ForEach(func(o plumbing.EncodedObject) error {
		_, err := c.st.SetEncodedObject(o)
		return err
	})
	if err != nil {
		t.Fatalf("copy remote objects: %v", err)
	}
	return c
}

// NewPushClientOn is a client whose scratch store is st, which must hold the trees of the commits it builds on.
func NewPushClientOn(t *testing.T, remote *gogit.Repository, st *memory.Storage) *PushClient {
	t.Helper()
	return &PushClient{t: t, st: st, remote: remote}
}

// Commit makes a commit on parent (zero for a root) that sets files, a map of path to content; an empty content
// removes the path.
func (c *PushClient) Commit(parent plumbing.Hash, files map[string]string, msg string) plumbing.Hash {
	c.t.Helper()
	flat := map[string]object.TreeEntry{}
	var parents []plumbing.Hash
	if !parent.IsZero() {
		parents = append(parents, parent)
		pc, err := object.GetCommit(c.st, parent)
		if err != nil {
			c.t.Fatalf("read parent: %v", err)
		}
		tree, err := pc.Tree()
		if err != nil {
			c.t.Fatalf("read parent tree: %v", err)
		}
		err = tree.Files().ForEach(func(f *object.File) error {
			flat[f.Name] = object.TreeEntry{Name: f.Name, Mode: f.Mode, Hash: f.Hash}
			return nil
		})
		if err != nil {
			c.t.Fatalf("list parent files: %v", err)
		}
	}
	for p, content := range files {
		if content == "" {
			delete(flat, p)
			continue
		}
		flat[p] = object.TreeEntry{Name: p, Mode: filemode.Regular, Hash: c.write(c.blob(content))}
	}
	tree := c.buildTree(flat, "")
	c.clock++
	sig := object.Signature{Name: "Tester", Email: "tester@example.com", When: time.Unix(1_700_000_000+c.clock, 0).UTC()}
	return c.write(&object.Commit{Author: sig, Committer: sig, Message: msg, TreeHash: tree, ParentHashes: parents})
}

// Tag makes an annotated tag of target.
func (c *PushClient) Tag(target plumbing.Hash, name string) plumbing.Hash {
	c.t.Helper()
	sig := object.Signature{Name: "Tester", Email: "tester@example.com", When: time.Unix(1_700_000_000, 0).UTC()}
	return c.write(&object.Tag{Name: name, Tagger: sig, Message: name, TargetType: plumbing.CommitObject, Target: target})
}

func (c *PushClient) blob(content string) object.Object { return rawBlob(content) }

type rawBlob string

func (b rawBlob) Type() plumbing.ObjectType { return plumbing.BlobObject }
func (b rawBlob) ID() plumbing.Hash         { return plumbing.ZeroHash }
func (b rawBlob) Decode(plumbing.EncodedObject) error {
	return nil
}
func (b rawBlob) Encode(o plumbing.EncodedObject) error {
	o.SetType(plumbing.BlobObject)
	w, err := o.Writer()
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, string(b)); err != nil {
		return err
	}
	return w.Close()
}

func (c *PushClient) write(o object.Object) plumbing.Hash {
	c.t.Helper()
	h := writeObject(c.t, c.st, o)
	c.created = append(c.created, h)
	return h
}

// buildTree writes the tree of the files under dir, bottom up.
func (c *PushClient) buildTree(flat map[string]object.TreeEntry, dir string) plumbing.Hash {
	byName := map[string]object.TreeEntry{}
	subdirs := map[string]bool{}
	for p, e := range flat {
		rel := p
		if dir != "" {
			rest, ok := strings.CutPrefix(p, dir+"/")
			if !ok {
				continue
			}
			rel = rest
		}
		if first, _, nested := strings.Cut(rel, "/"); nested {
			subdirs[first] = true
		} else {
			byName[rel] = object.TreeEntry{Name: rel, Mode: e.Mode, Hash: e.Hash}
		}
	}
	for name := range subdirs {
		byName[name] = object.TreeEntry{Name: name, Mode: filemode.Dir, Hash: c.buildTree(flat, path.Join(dir, name))}
	}
	entries := make([]object.TreeEntry, 0, len(byName))
	for _, e := range byName {
		entries = append(entries, e)
	}
	sort.Sort(object.TreeEntrySorter(entries))
	return c.write(&object.Tree{Entries: entries})
}

// Pack is a pack of what the client made that the remote lacks.
func (c *PushClient) Pack() []byte {
	c.t.Helper()
	var hs []plumbing.Hash
	for _, h := range c.created {
		if c.remote.Storer.HasEncodedObject(h) != nil {
			hs = append(hs, h)
		}
	}
	var buf bytes.Buffer
	if _, err := packfile.NewEncoder(&buf, c.st, false).Encode(hs, 0); err != nil {
		c.t.Fatalf("encode pack: %v", err)
	}
	return buf.Bytes()
}

// Request is the receive-pack request body pushing cmds with the client's pack, asking for report-status.
func (c *PushClient) Request(cmds ...*packp.Command) []byte {
	c.t.Helper()
	req := packp.NewReferenceUpdateRequest()
	_ = req.Capabilities.Set(capability.ReportStatus)
	req.Commands = cmds
	req.Packfile = io.NopCloser(bytes.NewReader(c.Pack()))
	if allDeletes(cmds) {
		req.Packfile = nil
	}
	var body bytes.Buffer
	if err := req.Encode(&body); err != nil {
		c.t.Fatalf("encode request: %v", err)
	}
	return body.Bytes()
}

func allDeletes(cmds []*packp.Command) bool {
	for _, cmd := range cmds {
		if cmd.Action() != packp.Delete {
			return false
		}
	}
	return true
}
