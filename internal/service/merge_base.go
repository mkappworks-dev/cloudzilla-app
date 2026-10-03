package service

import (
	"container/heap"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// isAncestor reports whether b reaches a, as `git merge-base --is-ancestor a b`
// does.
func isAncestor(repo *gogit.Repository, a, b *object.Commit) (bool, error) {
	if a.Hash == b.Hash {
		return true, nil
	}
	w := newPaintWalk(repo)
	na := w.start(a, fromA)
	nb := w.start(b, fromB)
	for na.flags&fromB == 0 {
		// a reaching b rules out b reaching a, and a stale commit lies below one
		// a reaches, so it can't lead to a either.
		if nb.flags&fromA != 0 || !w.hasLive(fromB) {
			return false, nil
		}
		if err := w.step(); err != nil {
			return false, err
		}
	}
	return true, nil
}

// findMergeBase returns the best common ancestor of a and b, as `git merge-base`
// does. Criss-cross histories have several; the newest is returned.
func findMergeBase(repo *gogit.Repository, a, b *object.Commit) (*object.Commit, error) {
	bases, err := mergeBases(repo, a, b)
	if err != nil {
		return nil, err
	}
	if len(bases) == 0 {
		return nil, ErrNoCommonAncestor
	}
	newest, tied := bases[0], false
	for _, c := range bases[1:] {
		switch {
		case c.Committer.When.After(newest.Committer.When):
			newest, tied = c, false
		case c.Committer.When.Equal(newest.Committer.When):
			tied = true
		}
	}
	if !tied {
		return newest, nil
	}
	// Ties keep go-git's pick, which only its walk through all of one side's
	// history reproduces. They need criss-cross merges from one second.
	bases, err = a.MergeBase(b)
	if err != nil {
		return nil, err
	}
	if len(bases) == 0 {
		return nil, ErrNoCommonAncestor
	}
	return bases[0], nil
}

// mergeBases returns the common ancestors of a and b that no other one reaches,
// as git's get_merge_bases does.
func mergeBases(repo *gogit.Repository, a, b *object.Commit) ([]*object.Commit, error) {
	if a.Hash == b.Hash {
		return []*object.Commit{a}, nil
	}
	w := newPaintWalk(repo)
	na := w.start(a, fromA)
	nb := w.start(b, fromB)
	for w.busy() {
		// Every common ancestor lies below a tip that the other reaches.
		if na.flags&fromB != 0 {
			return []*object.Commit{a}, nil
		}
		if nb.flags&fromA != 0 {
			return []*object.Commit{b}, nil
		}
		if err := w.step(); err != nil {
			return nil, err
		}
	}
	var bases []*object.Commit
	for _, n := range w.common {
		// Skew or a same-second tie can pop a common ancestor before a common
		// descendant of it, which then marks it stale.
		if n.flags&stale == 0 {
			bases = append(bases, n.commit)
		}
	}
	if len(bases) < 2 {
		return bases, nil
	}
	return independent(repo, bases)
}

// independent returns the commits that none of the others reaches, as git's
// remove_redundant does.
func independent(repo *gogit.Repository, commits []*object.Commit) ([]*object.Commit, error) {
	redundant := make([]bool, len(commits))
	for i, c := range commits {
		if redundant[i] {
			continue
		}
		w := newPaintWalk(repo)
		self := w.start(c, fromA)
		others := map[int]*paintNode{}
		for j, o := range commits {
			if j != i && !redundant[j] {
				others[j] = w.start(o, fromB)
			}
		}
		for w.busy() {
			if err := w.step(); err != nil {
				return nil, err
			}
		}
		if self.flags&fromB != 0 {
			redundant[i] = true
		}
		for j, n := range others {
			if n.flags&fromA != 0 {
				redundant[j] = true
			}
		}
	}
	var out []*object.Commit
	for i, c := range commits {
		if !redundant[i] {
			out = append(out, c)
		}
	}
	return out, nil
}

const (
	fromA uint8 = 1 << iota
	fromB
	stale // below a commit both sides reach
)

// paintWalk is git's paint_down_to_common: it walks both sides newest first,
// marking the commits each reaches, until every queued commit is below one
// both reach. Unlike commitRange it never stops on a date, so commits dated
// before their parents only make it read further. Like git without a
// commit-graph, it can read history made within one second to the root.
type paintWalk struct {
	repo   *gogit.Repository
	nodes  map[plumbing.Hash]*paintNode
	queue  paintQueue
	pushes int
	common []*paintNode // popped as reached from both sides, in that order
}

type paintNode struct {
	commit *object.Commit
	flags  uint8
	queued bool
	order  int
	common bool
}

func newPaintWalk(repo *gogit.Repository) *paintWalk {
	return &paintWalk{repo: repo, nodes: map[plumbing.Hash]*paintNode{}}
}

func (w *paintWalk) start(c *object.Commit, f uint8) *paintNode {
	n := w.nodes[c.Hash]
	if n == nil {
		n = &paintNode{commit: c}
		w.nodes[c.Hash] = n
	}
	w.mark(n, f)
	return n
}

// mark gives n the flags f. A commit is queued once at a time, and its pop
// reads the flags it has by then, which is what git's duplicate entries do.
func (w *paintWalk) mark(n *paintNode, f uint8) {
	if n.flags&f == f {
		return
	}
	n.flags |= f
	if !n.queued {
		n.queued = true
		n.order = w.pushes
		w.pushes++
		heap.Push(&w.queue, n)
	}
}

// step pops the newest queued commit and passes its flags to its parents.
func (w *paintWalk) step() error {
	n := heap.Pop(&w.queue).(*paintNode)
	n.queued = false
	f := n.flags
	if f == fromA|fromB {
		if !n.common {
			n.common = true
			w.common = append(w.common, n)
		}
		f |= stale
	}
	for _, h := range n.commit.ParentHashes {
		p := w.nodes[h]
		if p == nil {
			c, err := w.repo.CommitObject(h)
			if err != nil {
				return err
			}
			p = &paintNode{commit: c}
			w.nodes[h] = p
		}
		w.mark(p, f)
	}
	return nil
}

// busy reports whether a queued commit isn't known to be below one both sides
// reach.
func (w *paintWalk) busy() bool { return w.hasLive(fromA | fromB) }

// hasLive reports whether a queued commit that isn't stale has any of flags f.
func (w *paintWalk) hasLive(f uint8) bool {
	for _, n := range w.queue {
		if n.flags&f != 0 && n.flags&stale == 0 {
			return true
		}
	}
	return false
}

// paintQueue pops the newest commit first, and commits from the same second in
// the order they were queued, as git's commit-date queue does.
type paintQueue []*paintNode

func (q paintQueue) Len() int { return len(q) }

func (q paintQueue) Less(i, j int) bool {
	if a, b := q[i].commit.Committer.When.Unix(), q[j].commit.Committer.When.Unix(); a != b {
		return a > b
	}
	return q[i].order < q[j].order
}

func (q paintQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }

func (q *paintQueue) Push(x any) { *q = append(*q, x.(*paintNode)) }

func (q *paintQueue) Pop() any {
	old := *q
	n := old[len(old)-1]
	*q = old[:len(old)-1]
	return n
}
