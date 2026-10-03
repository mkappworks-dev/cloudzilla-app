package service

import (
	"container/heap"
	"math"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// rangeWalkSlop is how many more commits a range walk reads once it looks
// done, so a few commits dated before their parents can't end it early. git's
// revision walk allows the same.
const rangeWalkSlop = 5

// commitRange returns the commits reachable from tip but not from old, newest
// first: what `git rev-list old..tip` lists. Like git, it walks both sides in
// commit-date order and stops once every commit left to visit is reachable from
// old and older than every commit it kept, so it reads the history since the
// two diverged rather than all of old's. It can't stop among commits from the
// same second as one it kept, so history made within one second, as scripts
// make it, is read to the root.
func commitRange(repo *gogit.Repository, old, tip plumbing.Hash) ([]*object.Commit, error) {
	w := rangeWalk{repo: repo, nodes: map[plumbing.Hash]*rangeNode{}}
	if err := w.add(old, true); err != nil {
		return nil, err
	}
	if err := w.add(tip, false); err != nil {
		return nil, err
	}
	var kept []*rangeNode
	oldestKept := int64(math.MaxInt64)
	slop := rangeWalkSlop
	for w.queue.Len() > 0 {
		n := heap.Pop(&w.queue).(*rangeNode)
		for _, p := range n.commit.ParentHashes {
			if err := w.add(p, n.excluded); err != nil {
				return nil, err
			}
		}
		if !n.excluded {
			kept = append(kept, n)
			oldestKept = min(oldestKept, n.when())
			continue
		}
		if w.settled(oldestKept) {
			slop--
		} else {
			slop = rangeWalkSlop
		}
		if slop == 0 {
			break
		}
	}
	var out []*object.Commit
	for _, n := range kept {
		if !n.excluded {
			out = append(out, n.commit)
		}
	}
	return out, nil
}

type rangeWalk struct {
	repo  *gogit.Repository
	nodes map[plumbing.Hash]*rangeNode
	queue rangeQueue
}

type rangeNode struct {
	commit   *object.Commit
	excluded bool // reachable from old
	order    int
}

func (n *rangeNode) when() int64 { return n.commit.Committer.When.Unix() }

func (w *rangeWalk) add(h plumbing.Hash, excluded bool) error {
	if n, ok := w.nodes[h]; ok {
		if excluded {
			w.exclude(n)
		}
		return nil
	}
	c, err := w.repo.CommitObject(h)
	if err != nil {
		return err
	}
	n := &rangeNode{commit: c, excluded: excluded, order: len(w.nodes)}
	w.nodes[h] = n
	heap.Push(&w.queue, n)
	return nil
}

// exclude also marks the ancestors of n the walk has already loaded: when dates
// don't order two commits, one can be kept before old's side reaches it.
func (w *rangeWalk) exclude(n *rangeNode) {
	stack := []*rangeNode{n}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.excluded {
			continue
		}
		n.excluded = true
		for _, p := range n.commit.ParentHashes {
			if pn, ok := w.nodes[p]; ok {
				stack = append(stack, pn)
			}
		}
	}
}

// settled reports whether every queued commit is reachable from old and dated
// before every kept commit, so that none of them can be a kept commit's
// descendant unless dates are skewed.
func (w *rangeWalk) settled(oldestKept int64) bool {
	for _, n := range w.queue {
		if !n.excluded || n.when() >= oldestKept {
			return false
		}
	}
	return true
}

// rangeQueue pops the newest commit first. Commits from the same second leave
// in the order they were queued, which keeps the output stable.
type rangeQueue []*rangeNode

func (q rangeQueue) Len() int { return len(q) }

func (q rangeQueue) Less(i, j int) bool {
	if a, b := q[i].when(), q[j].when(); a != b {
		return a > b
	}
	return q[i].order < q[j].order
}

func (q rangeQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }

func (q *rangeQueue) Push(x any) { *q = append(*q, x.(*rangeNode)) }

func (q *rangeQueue) Pop() any {
	old := *q
	n := old[len(old)-1]
	*q = old[:len(old)-1]
	return n
}
