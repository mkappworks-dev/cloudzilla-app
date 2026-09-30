package gittransport_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

var (
	tagRef  = plumbing.NewTagReferenceName("v1")
	missing = plumbing.NewHash("1234567890123456789012345678901234567890")
)

// objects writes git objects to st: a client's store for what it pushes, or
// the repo's for what the repo already holds.
type objects struct {
	t  *testing.T
	st storer.EncodedObjectStorer
}

// clock dates each commit after the last, as real history is: the check walks
// commits newest first.
var clock int64

func (o objects) put(encode func(plumbing.EncodedObject) error) plumbing.Hash {
	o.t.Helper()
	obj := o.st.NewEncodedObject()
	if err := encode(obj); err != nil {
		o.t.Fatalf("encode object: %v", err)
	}
	h, err := o.st.SetEncodedObject(obj)
	if err != nil {
		o.t.Fatalf("store object: %v", err)
	}
	return h
}

func (o objects) blob(content string) plumbing.Hash {
	return o.put(func(obj plumbing.EncodedObject) error {
		obj.SetType(plumbing.BlobObject)
		w, err := obj.Writer()
		if err != nil {
			return err
		}
		if _, err := io.WriteString(w, content); err != nil {
			return err
		}
		return w.Close()
	})
}

func (o objects) tree(entries ...object.TreeEntry) plumbing.Hash {
	return o.put((&object.Tree{Entries: entries}).Encode)
}

func (o objects) commit(msg string, tree plumbing.Hash, parents ...plumbing.Hash) plumbing.Hash {
	clock++
	sig := object.Signature{Name: "Tester", Email: "tester@example.com", When: time.Unix(clock, 0).UTC()}
	return o.put((&object.Commit{Author: sig, Committer: sig, Message: msg, TreeHash: tree, ParentHashes: parents}).Encode)
}

func (o objects) tag(target plumbing.Hash, typ plumbing.ObjectType) plumbing.Hash {
	sig := object.Signature{Name: "Tester", Email: "tester@example.com", When: time.Unix(0, 0).UTC()}
	return o.put((&object.Tag{Name: "v1", Tagger: sig, Message: "v1", TargetType: typ, Target: target}).Encode)
}

// pack is a pack of hs, which st must hold.
func (o objects) pack(hs ...plumbing.Hash) []byte {
	o.t.Helper()
	var buf bytes.Buffer
	if _, err := packfile.NewEncoder(&buf, o.st, false).Encode(hs, 0); err != nil {
		o.t.Fatalf("encode pack: %v", err)
	}
	return buf.Bytes()
}

func file(name string, h plumbing.Hash) object.TreeEntry {
	return object.TreeEntry{Name: name, Mode: filemode.Regular, Hash: h}
}

func dir(name string, h plumbing.Hash) object.TreeEntry {
	return object.TreeEntry{Name: name, Mode: filemode.Dir, Hash: h}
}

func gitlink(name string, h plumbing.Hash) object.TreeEntry {
	return object.TreeEntry{Name: name, Mode: filemode.Submodule, Hash: h}
}

// push is a push of one ref to a new value, carrying the objects in pack.
type push struct {
	ref  plumbing.ReferenceName
	to   plumbing.Hash
	pack []byte
}

// send pushes p from the ref's current value, or as a create, and returns the
// ref's status and whether vet saw it.
func (r *pushRepo) send(t *testing.T, p push) (status string, vetted bool) {
	t.Helper()
	vet := func(*packp.Command) error {
		vetted = true
		return nil
	}
	r.pack = p.pack
	st, err := r.receive(r.advertise(t, vet), &packp.Command{Name: p.ref, Old: r.ref(t, p.ref), New: p.to})
	if err != nil {
		t.Errorf("ReceivePack: %v; a refused ref belongs in the report status only", err)
	}
	return refStatus(t, st, p.ref), vetted
}

// ref is name's value, or the zero hash if it doesn't exist.
func (r *pushRepo) ref(t *testing.T, name plumbing.ReferenceName) plumbing.Hash {
	t.Helper()
	ref, err := r.repo.Storer.Reference(name)
	if err == plumbing.ErrReferenceNotFound {
		return plumbing.ZeroHash
	}
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return ref.Hash()
}

// expectRefused sends p and checks it was refused with want before vet saw it.
func (r *pushRepo) expectRefused(t *testing.T, p push, want error) {
	t.Helper()
	before := r.ref(t, p.ref)
	status, vetted := r.send(t, p)
	if status != want.Error() {
		t.Errorf("%s status = %q, want %q", p.ref, status, want.Error())
	}
	if vetted {
		t.Errorf("vet called for %s", p.ref)
	}
	if got := r.ref(t, p.ref); got != before {
		t.Errorf("%s = %s, want %s", p.ref, got, before)
	}
}

func (r *pushRepo) expectApplied(t *testing.T, p push) {
	t.Helper()
	if status, _ := r.send(t, p); status != "ok" {
		t.Errorf("%s status = %q, want ok", p.ref, status)
	}
	if got := r.ref(t, p.ref); got != p.to {
		t.Errorf("%s = %s, want %s", p.ref, got, p.to)
	}
}

// Every object reachable from the new value must be present, not just the
// object itself.
func TestNewServer_PushMissingObjects_Refused(t *testing.T) {
	for _, tc := range []struct {
		name string
		push func(r *pushRepo, c objects) push
	}{
		{"parent", func(r *pushRepo, c objects) push {
			tip := c.commit("tip", c.tree(), r.base, missing)
			return push{mainRef, tip, c.pack(tip)}
		}},
		{"tree", func(r *pushRepo, c objects) push {
			tip := c.commit("tip", c.tree(file("a", c.blob("a"))), r.base)
			return push{mainRef, tip, c.pack(tip)}
		}},
		{"subtree", func(r *pushRepo, c objects) push {
			root := c.tree(dir("d", c.tree(file("a", c.blob("a")))))
			tip := c.commit("tip", root, r.base)
			return push{mainRef, tip, c.pack(tip, root)}
		}},
		{"blob", func(r *pushRepo, c objects) push {
			root := c.tree(file("a", c.blob("a")))
			tip := c.commit("tip", root, r.base)
			return push{mainRef, tip, c.pack(tip, root)}
		}},
		{"earlier pushed commit's tree", func(r *pushRepo, c objects) push {
			parent := c.commit("parent", c.tree(file("a", c.blob("a"))), r.base)
			tip := c.commit("tip", c.tree(), parent)
			return push{mainRef, tip, c.pack(tip, parent)}
		}},
		{"tag target", func(r *pushRepo, c objects) push {
			tag := c.tag(missing, plumbing.CommitObject)
			return push{tagRef, tag, c.pack(tag)}
		}},
		{"tagged commit's tree", func(r *pushRepo, c objects) push {
			tip := c.commit("tip", c.tree(file("a", c.blob("a"))), r.base)
			tag := c.tag(tip, plumbing.CommitObject)
			return push{tagRef, tag, c.pack(tag, tip)}
		}},
		{"tagged tree's blob", func(r *pushRepo, c objects) push {
			root := c.tree(file("a", c.blob("a")))
			tag := c.tag(root, plumbing.TreeObject)
			return push{tagRef, tag, c.pack(tag, root)}
		}},
		// A submodule's commit needn't be here, so the parent having its hash
		// says nothing of a file with that hash.
		{"file named by a parent's submodule", func(r *pushRepo, c objects) push {
			repo := objects{c.t, r.repo.Storer}
			old := repo.commit("old", repo.tree(gitlink("s", missing)), r.base)
			r.set(c.t, mainRef, old)
			root := c.tree(file("f", missing), gitlink("s", missing))
			tip := c.commit("tip", root, old)
			return push{mainRef, tip, c.pack(tip, root)}
		}},
		// A blob entry is only checked for presence, so it says nothing of what
		// a directory with its hash reaches.
		{"directory named by a parent's blob", func(r *pushRepo, c objects) push {
			repo := objects{c.t, r.repo.Storer}
			sub := repo.tree(file("a", missing))
			old := repo.commit("old", repo.tree(file("f", sub)), r.base)
			r.set(c.t, mainRef, old)
			root := c.tree(dir("d", sub), file("f", sub))
			tip := c.commit("tip", root, old)
			return push{mainRef, tip, c.pack(tip, root)}
		}},
		{"directory named by a blob beside it", func(r *pushRepo, c objects) push {
			sub := objects{c.t, r.repo.Storer}.tree(file("a", missing))
			root := c.tree(file("f", sub), dir("g", sub))
			tip := c.commit("tip", root, r.base)
			return push{mainRef, tip, c.pack(tip, root)}
		}},
		// The tip swaps two directories of its parent's. Checked against the
		// parent's tree, each defers its missing file to the parent's check,
		// which must not then skip them as already checked.
		{"file under directories the tip swaps", func(r *pushRepo, c objects) push {
			one := c.tree(file("a", c.blob("1")), file("x", missing))
			two := c.tree(file("a", c.blob("2")), file("x", missing))
			in := func(sub plumbing.Hash, side string) plumbing.Hash {
				return c.tree(dir("p", sub), file("side", c.blob(side)))
			}
			parent := c.commit("parent", c.tree(dir("d1", in(one, "d1")), dir("d2", in(two, "d2"))), r.base)
			tip := c.commit("tip", c.tree(dir("d1", in(two, "d1")), dir("d2", in(one, "d2"))), parent)
			return push{mainRef, tip, testutil.PackAll(c.t, c.st)}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newPushRepo(t)
			r.expectRefused(t, tc.push(r, objects{t, memory.NewStorage()}), gittransport.ErrMissingObjects)
		})
	}
}

// go-git stores a pack before it updates any ref, so a refused push leaves its
// objects behind: that the repo has an object doesn't make it connected.
func TestNewServer_PushOntoRefusedPushObjects_Refused(t *testing.T) {
	for _, tc := range []struct {
		name string
		push func(r *pushRepo, c objects, left, leftTree plumbing.Hash) push
	}{
		{"same commit", func(r *pushRepo, c objects, left, _ plumbing.Hash) push {
			return push{topicRef, left, c.pack(c.commit("unrelated", c.tree(), r.base))}
		}},
		{"as parent", func(r *pushRepo, c objects, left, _ plumbing.Hash) push {
			tip := c.commit("tip", c.tree(), left)
			return push{topicRef, tip, c.pack(tip)}
		}},
		{"its tree as a subtree", func(r *pushRepo, c objects, _, leftTree plumbing.Hash) push {
			root := c.tree(dir("d", leftTree))
			tip := c.commit("tip", root, r.base)
			return push{topicRef, tip, c.pack(tip, root)}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newPushRepo(t)
			first := objects{t, memory.NewStorage()}
			leftTree := first.tree(file("a", first.blob("a")))
			left := first.commit("left", leftTree, r.base)
			r.expectRefused(t, push{mainRef, left, first.pack(left, leftTree)}, gittransport.ErrMissingObjects)

			r.expectRefused(t, tc.push(r, objects{t, memory.NewStorage()}, left, leftTree), gittransport.ErrMissingObjects)
		})
	}
}

func TestNewServer_ConnectedPush_Applies(t *testing.T) {
	for _, tc := range []struct {
		name string
		push func(r *pushRepo, c objects) push
	}{
		{"new branch at an existing commit", func(r *pushRepo, c objects) push {
			return push{topicRef, r.base, c.pack(c.commit("unrelated", c.tree(), r.base))}
		}},
		{"history with subtrees", func(r *pushRepo, c objects) push {
			parent := c.commit("parent", c.tree(dir("d", c.tree(file("a", c.blob("a"))))), r.base)
			tip := c.commit("tip", c.tree(dir("d", c.tree(file("a", c.blob("a")), file("b", c.blob("b"))))), parent)
			return push{mainRef, tip, testutil.PackAll(c.t, c.st)}
		}},
		// A submodule's commit lives in another repo.
		{"submodule", func(r *pushRepo, c objects) push {
			root := c.tree(gitlink("sub", missing))
			tip := c.commit("tip", root, r.base)
			return push{mainRef, tip, c.pack(tip, root)}
		}},
		{"annotated tag", func(r *pushRepo, c objects) push {
			tip := c.commit("tip", c.tree(), r.base)
			tag := c.tag(tip, plumbing.CommitObject)
			return push{tagRef, tag, c.pack(tag, tip)}
		}},
		{"tag at a tree", func(r *pushRepo, c objects) push {
			root := c.tree(file("a", c.blob("a")))
			return push{tagRef, root, testutil.PackAll(c.t, c.st)}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newPushRepo(t)
			r.expectApplied(t, tc.push(r, objects{t, memory.NewStorage()}))
		})
	}
}

// Like git, the check takes what the repo's refs already reach as connected
// rather than walking all history, so a push costs what it adds. A repo whose
// old history lost objects shows where the walk stops.
func TestNewServer_CheckStopsAtWhatRefsReach(t *testing.T) {
	for _, tc := range []struct {
		name string
		push func(t *testing.T, r *pushRepo, repo, c objects) push
	}{
		{"branch tip", func(t *testing.T, r *pushRepo, repo, c objects) push {
			old := repo.commit("old", repo.tree(), missing)
			r.set(t, mainRef, old)
			tip := c.commit("tip", c.tree(), old)
			return push{mainRef, tip, c.pack(tip)}
		}},
		{"commit below a branch tip", func(t *testing.T, r *pushRepo, repo, c objects) push {
			old := repo.commit("old", repo.tree(), missing)
			r.set(t, mainRef, repo.commit("newer", repo.tree(), repo.commit("mid", repo.tree(), old)))
			tip := c.commit("tip", c.tree(), old)
			return push{topicRef, tip, c.pack(tip)}
		}},
		{"commit below a tag", func(t *testing.T, r *pushRepo, repo, c objects) push {
			old := repo.commit("old", repo.tree(), missing)
			r.set(t, tagRef, repo.tag(repo.commit("newer", repo.tree(), old), plumbing.CommitObject))
			tip := c.commit("tip", c.tree(), old)
			return push{topicRef, tip, c.pack(tip)}
		}},
		{"unchanged subtree", func(t *testing.T, r *pushRepo, repo, c objects) push {
			sub := repo.tree(file("a", missing))
			old := repo.commit("old", repo.tree(dir("d", sub)), r.base)
			r.set(t, mainRef, old)
			root := c.tree(dir("d", sub), file("e", c.blob("e")))
			tip := c.commit("tip", root, old)
			return push{mainRef, tip, c.pack(tip, root, c.blob("e"))}
		}},
		{"unchanged file in a changed directory", func(t *testing.T, r *pushRepo, repo, c objects) push {
			old := repo.commit("old", repo.tree(dir("d", repo.tree(file("a", missing)))), r.base)
			r.set(t, mainRef, old)
			sub := c.tree(file("a", missing), file("b", c.blob("b")))
			root := c.tree(dir("d", sub))
			tip := c.commit("tip", root, old)
			return push{mainRef, tip, c.pack(tip, root, sub, c.blob("b"))}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newPushRepo(t)
			r.expectApplied(t, tc.push(t, r, objects{t, r.repo.Storer}, objects{t, memory.NewStorage()}))
		})
	}
}

// A push usually builds on the ref it moves, so its check needn't read the
// repo's other refs, of which there may be many thousands.
func TestNewServer_PushOntoItsRef_ReadsNoOtherRef(t *testing.T) {
	r := newPushRepo(t)
	r.set(t, tagRef, r.base)
	c := objects{t, memory.NewStorage()}
	parent := c.commit("parent", c.tree(file("a", c.blob("a"))), r.base)
	tip := c.commit("tip", c.tree(file("a", c.blob("b"))), parent)
	r.pack = testutil.PackAll(t, c.st)
	sess := r.advertise(t, nil)
	tags := filepath.Join(r.dir, "refs", "tags")
	if err := os.Chmod(tags, 0); err != nil {
		t.Fatalf("hide tags: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(tags, 0o755) })

	status, err := r.receive(sess, &packp.Command{Name: mainRef, Old: r.base, New: tip})

	if err != nil {
		t.Fatalf("ReceivePack: %v", err)
	}
	if got := refStatus(t, status, mainRef); got != "ok" {
		t.Errorf("main status = %q, want ok", got)
	}
}

// Only a value a ref holds vouches for history. A push names whatever old
// value it likes, and what one ref's check finds informs the rest of the push.
func TestNewServer_OldValueTheRefDoesNotHold_VouchesForNothing(t *testing.T) {
	r := newPushRepo(t)
	first := objects{t, memory.NewStorage()}
	left := first.commit("left", first.tree(), r.base, missing)
	r.expectRefused(t, push{topicRef, left, first.pack(left)}, gittransport.ErrMissingObjects)
	c := objects{t, memory.NewStorage()}
	onLeft := c.commit("on left", c.tree(), left)
	onTop := c.commit("on top", c.tree(), onLeft)
	r.pack = c.pack(onLeft, onTop)

	status, err := r.receive(r.advertise(t, nil),
		&packp.Command{Name: mainRef, Old: left, New: onLeft},
		&packp.Command{Name: topicRef, Old: plumbing.ZeroHash, New: onTop},
	)

	if err != nil {
		t.Errorf("ReceivePack: %v; a refused ref belongs in the report status only", err)
	}
	if got := refStatus(t, status, topicRef); got != gittransport.ErrMissingObjects.Error() {
		t.Errorf("topic status = %q, want %q", got, gittransport.ErrMissingObjects.Error())
	}
	if got := r.ref(t, topicRef); !got.IsZero() {
		t.Errorf("topic = %s, want none", got)
	}
	if got := r.ref(t, mainRef); got != r.base {
		t.Errorf("main = %s, want %s", got, r.base)
	}
}

// Like git ("trying to write non-commit object to branch"): a branch is read
// as a commit everywhere. Tags may point at any object.
func TestNewServer_BranchToNonCommit_Refused(t *testing.T) {
	for _, tc := range []struct {
		name string
		push func(r *pushRepo, c objects) push
	}{
		{"update to tree", func(r *pushRepo, c objects) push {
			root := c.tree(file("a", c.blob("a")))
			return push{mainRef, root, testutil.PackAll(c.t, c.st)}
		}},
		{"create at blob", func(r *pushRepo, c objects) push {
			b := c.blob("a")
			return push{topicRef, b, c.pack(b)}
		}},
		{"create at tag", func(r *pushRepo, c objects) push {
			tag := c.tag(r.base, plumbing.CommitObject)
			return push{topicRef, tag, c.pack(tag)}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newPushRepo(t)
			r.expectRefused(t, tc.push(r, objects{t, memory.NewStorage()}), gittransport.ErrNonCommitBranch)
		})
	}
}
