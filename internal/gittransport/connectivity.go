package gittransport

import (
	"container/heap"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

// connectivity is git's check_connected for receive-pack, which go-git skips:
// every object a pushed value reaches must be in the repo. What the refs
// reached before the push is taken as connected, so the walk covers what the
// push added.
//
// An object being present proves nothing: go-git stores a pack before any ref
// is vetted, so a refused push leaves its objects behind. The walk stops only
// at what refs reach.
type connectivity struct {
	s storer.Storer
	// pushed holds the commits the push's pack stored.
	pushed map[plumbing.Hash]struct{}
	// have holds commits known connected: the refs' once read, and each
	// accepted value's.
	have     []*commitInfo
	haveRefs bool
	commits  map[plumbing.Hash]*commitInfo
	// trees holds trees whose whole closure is present; blobs, objects present
	// as blobs. They're apart because a blob entry is only checked for
	// presence, which says nothing of a tree with its hash.
	trees, blobs map[plumbing.Hash]struct{}
}

type commitInfo struct {
	hash, tree plumbing.Hash
	parents    []plumbing.Hash
	when       int64
}

func newConnectivity(s storer.Storer) *connectivity {
	return &connectivity{
		s:       s,
		pushed:  make(map[plumbing.Hash]struct{}),
		commits: make(map[plumbing.Hash]*commitInfo),
		trees:   make(map[plumbing.Hash]struct{}),
		blobs:   make(map[plumbing.Hash]struct{}),
	}
}

// check returns an error matching plumbing.ErrObjectNotFound if an object obj
// reaches is missing. from is the value obj's ref holds, or zero.
func (c *connectivity) check(obj plumbing.EncodedObject, from plumbing.Hash) error {
	for obj.Type() == plumbing.TagObject {
		tag, err := object.DecodeTag(c.s, obj)
		if err != nil {
			return err
		}
		if obj, err = c.s.EncodedObject(plumbing.AnyObject, tag.Target); err != nil {
			return err
		}
	}
	switch obj.Type() {
	case plumbing.CommitObject:
		return c.checkCommit(obj.Hash(), from)
	case plumbing.TreeObject:
		return c.checkTree(obj.Hash(), nil)
	}
	return nil
}

func (c *connectivity) checkCommit(h, from plumbing.Hash) error {
	tip, err := c.commit(h)
	if err != nil {
		return err
	}
	added, ok := c.addedOnto(tip, from)
	if !ok {
		if err := c.loadRefs(); err != nil {
			return err
		}
		if added, err = c.added(tip); err != nil {
			return err
		}
	}
	// A commit's tree is checked against its parents' trees, which are
	// connected only once those parents are checked.
	for _, cm := range parentsFirst(added) {
		if err := c.checkTree(cm.tree, c.parentTrees(cm)); err != nil {
			return err
		}
	}
	c.have = append(c.have, tip)
	return nil
}

func (c *connectivity) loadRefs() error {
	if c.haveRefs {
		return nil
	}
	refs, err := c.s.IterReferences()
	if err != nil {
		return err
	}
	err = refs.ForEach(func(ref *plumbing.Reference) error {
		if ref.Type() == plumbing.HashReference {
			if cm := c.peel(ref.Hash()); cm != nil {
				c.have = append(c.have, cm)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	c.haveRefs = true
	return nil
}

// peel is the commit h is or tags, or nil if there is none.
func (c *connectivity) peel(h plumbing.Hash) *commitInfo {
	for {
		if cm, err := c.commit(h); err == nil {
			return cm
		}
		tag, err := object.GetTag(c.s, h)
		if err != nil {
			return nil
		}
		h = tag.Target
	}
}

func (c *connectivity) commit(h plumbing.Hash) (*commitInfo, error) {
	if cm, ok := c.commits[h]; ok {
		return cm, nil
	}
	commit, err := object.GetCommit(c.s, h)
	if err != nil {
		return nil, err
	}
	cm := &commitInfo{hash: h, tree: commit.TreeHash, parents: commit.ParentHashes, when: commit.Committer.When.Unix()}
	c.commits[h] = cm
	return cm, nil
}

// addedOnto is added for the usual push, whose tip sits on commits its pack
// stored, down to from, the value the ref holds; it spares reading every ref.
// It reports false for any other push.
func (c *connectivity) addedOnto(tip *commitInfo, from plumbing.Hash) ([]*commitInfo, bool) {
	if from.IsZero() {
		return nil, false
	}
	onto := c.peel(from)
	if onto == nil {
		return nil, false
	}
	if tip.hash == onto.hash {
		return nil, true
	}
	seen := map[plumbing.Hash]bool{tip.hash: true, onto.hash: true}
	var walked []*commitInfo
	stack := []*commitInfo{tip}
	for len(stack) > 0 {
		cm := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		walked = append(walked, cm)
		for _, p := range cm.parents {
			if seen[p] {
				continue
			}
			if _, ok := c.pushed[p]; !ok {
				return nil, false
			}
			seen[p] = true
			pc, err := c.commit(p)
			if err != nil {
				return nil, false
			}
			stack = append(stack, pc)
		}
	}
	return walked, true
}

// added returns the commits tip reaches and c.have doesn't, as git rev-list
// tip --not --all does. Both sides are read newest first, so the walk ends
// soon after passing the commits tip's history forked from; skewed dates only
// make it walk further.
func (c *connectivity) added(tip *commitInfo) ([]*commitInfo, error) {
	const (
		seen = 1 << iota
		reached
		popped
	)
	flags := make(map[plumbing.Hash]uint8)
	q := &commitQueue{}
	unreached := 0 // queued commits not known to be reached
	push := func(cm *commitInfo, f uint8) {
		flags[cm.hash] = seen | f
		if f&reached == 0 {
			unreached++
		}
		q.add(cm)
	}
	// reach marks h reached, along with the history of any commit already
	// popped as unreached.
	reach := func(h plumbing.Hash) {
		stack := []plumbing.Hash{h}
		for len(stack) > 0 {
			h := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			f := flags[h]
			if f&seen == 0 || f&reached != 0 {
				continue
			}
			flags[h] = f | reached
			if f&popped == 0 {
				unreached--
				continue
			}
			stack = append(stack, c.commits[h].parents...)
		}
	}

	for _, cm := range c.have {
		if flags[cm.hash] == 0 {
			push(cm, reached)
		}
	}
	if flags[tip.hash] == 0 {
		push(tip, 0)
	}
	var walked []*commitInfo
	for unreached > 0 {
		cm := heap.Pop(q).(queuedCommit).c
		f := flags[cm.hash]
		flags[cm.hash] = f | popped
		if f&reached == 0 {
			unreached--
			walked = append(walked, cm)
		}
		for _, p := range cm.parents {
			switch {
			case flags[p]&seen != 0:
				if f&reached != 0 {
					reach(p)
				}
			case f&reached != 0:
				// A commit missing from history the refs reach isn't this
				// push's doing.
				if pc, err := c.commit(p); err == nil {
					push(pc, reached)
				}
			default:
				pc, err := c.commit(p)
				if err != nil {
					return nil, err
				}
				push(pc, 0)
			}
		}
	}

	added := walked[:0]
	for _, cm := range walked {
		if flags[cm.hash]&reached == 0 {
			added = append(added, cm)
		}
	}
	return added, nil
}

// parentsFirst orders cs so that each commit follows its parents among them.
func parentsFirst(cs []*commitInfo) []*commitInfo {
	in := make(map[plumbing.Hash]*commitInfo, len(cs))
	for _, cm := range cs {
		in[cm.hash] = cm
	}
	done := make(map[plumbing.Hash]bool, len(cs))
	out := make([]*commitInfo, 0, len(cs))
	for _, cm := range cs {
		stack := []*commitInfo{cm}
		for len(stack) > 0 {
			top := stack[len(stack)-1]
			if done[top.hash] {
				stack = stack[:len(stack)-1]
				continue
			}
			ready := true
			for _, p := range top.parents {
				if pc, ok := in[p]; ok && !done[p] {
					stack = append(stack, pc)
					ready = false
				}
			}
			if ready {
				done[top.hash] = true
				out = append(out, top)
				stack = stack[:len(stack)-1]
			}
		}
	}
	return out
}

// parentTrees are the trees of cm's parents that can be read.
func (c *connectivity) parentTrees(cm *commitInfo) []*object.Tree {
	var trees []*object.Tree
	for _, p := range cm.parents {
		pc, ok := c.commits[p]
		if !ok {
			continue
		}
		if t, err := object.GetTree(c.s, pc.tree); err == nil {
			trees = append(trees, t)
		}
	}
	return trees
}

// checkTree checks what tree h reaches. bases are connected trees at the same
// path, from the commit's parents; what they hold is skipped, so the walk
// covers what changed.
func (c *connectivity) checkTree(h plumbing.Hash, bases []*object.Tree) error {
	if _, ok := c.trees[h]; ok {
		return nil
	}
	// known maps what bases hold to whether it's a tree. A submodule entry
	// vouches for nothing: its commit needn't be here.
	known := make(map[plumbing.Hash]bool)
	for _, b := range bases {
		if b.Hash == h {
			return nil
		}
		for _, e := range b.Entries {
			if e.Mode != filemode.Submodule {
				known[e.Hash] = e.Mode == filemode.Dir
			}
		}
	}
	tree, err := object.GetTree(c.s, h)
	if err != nil {
		return err
	}
	for _, e := range tree.Entries {
		if isTree, ok := known[e.Hash]; ok && isTree == (e.Mode == filemode.Dir) {
			continue
		}
		switch e.Mode {
		case filemode.Submodule:
			// The commit is in the submodule's repo.
		case filemode.Dir:
			var subs []*object.Tree
			for _, b := range bases {
				if sub, err := b.Tree(e.Name); err == nil {
					subs = append(subs, sub)
				}
			}
			if err := c.checkTree(e.Hash, subs); err != nil {
				return err
			}
		default:
			if _, ok := c.blobs[e.Hash]; ok {
				continue
			}
			if err := c.s.HasEncodedObject(e.Hash); err != nil {
				return err
			}
			c.blobs[e.Hash] = struct{}{}
		}
	}
	c.trees[h] = struct{}{}
	return nil
}

// commitQueue pops the newest commit first, and commits of equal date in the
// order they were added.
type commitQueue struct {
	items []queuedCommit
	added int
}

type queuedCommit struct {
	c   *commitInfo
	seq int
}

func (q *commitQueue) add(cm *commitInfo) {
	heap.Push(q, queuedCommit{cm, q.added})
	q.added++
}

func (q *commitQueue) Len() int { return len(q.items) }

func (q *commitQueue) Less(i, j int) bool {
	a, b := q.items[i], q.items[j]
	if a.c.when != b.c.when {
		return a.c.when > b.c.when
	}
	return a.seq < b.seq
}

func (q *commitQueue) Swap(i, j int) { q.items[i], q.items[j] = q.items[j], q.items[i] }

func (q *commitQueue) Push(x any) { q.items = append(q.items, x.(queuedCommit)) }

func (q *commitQueue) Pop() any {
	last := q.items[len(q.items)-1]
	q.items = q.items[:len(q.items)-1]
	return last
}
