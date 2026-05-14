package service

import (
	"context"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

// Mergeability is the composite mergeability summary used by the PR
// detail page sidebar. It only captures the raw git-level merge-base +
// tree-merge state; branch protection / required checks / required
// reviews are layered on top by the PR handler.
type Mergeability struct {
	BaseRef      string
	HeadRef      string
	Ahead        int    // commits in head not in base (excludes merge base)
	Behind       int    // commits in base not in head (excludes merge base)
	HasConflicts bool
	MergeBase    string // SHA, empty if no common ancestor
}

// Mergeability computes the merge-readiness composite for (base, head).
// Reuses the merge helpers in code_service_merge.go: resolveRef,
// findMergeBase, mergeTreesNoConflict. No new helpers are introduced.
func (s *CodeService) Mergeability(ctx context.Context, owner, repoName, base, head string) (Mergeability, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return Mergeability{}, err
	}
	baseCommit, _, err := resolveRef(repo, base)
	if err != nil {
		return Mergeability{}, err
	}
	headCommit, _, err := resolveRef(repo, head)
	if err != nil {
		return Mergeability{}, err
	}
	mb, err := findMergeBase(repo, baseCommit, headCommit)
	if err != nil {
		// findMergeBase returns errors.New("no common ancestor") literally.
		if err.Error() == "no common ancestor" {
			return Mergeability{BaseRef: base, HeadRef: head, HasConflicts: true}, nil
		}
		return Mergeability{}, err
	}
	ahead, err := countCommitsBetween(repo, mb, headCommit)
	if err != nil {
		return Mergeability{}, err
	}
	behind, err := countCommitsBetween(repo, mb, baseCommit)
	if err != nil {
		return Mergeability{}, err
	}
	_, ok, err := mergeTreesNoConflict(repo, mb, baseCommit, headCommit)
	if err != nil {
		return Mergeability{}, err
	}
	return Mergeability{
		BaseRef:      base,
		HeadRef:      head,
		Ahead:        ahead,
		Behind:       behind,
		HasConflicts: !ok,
		MergeBase:    mb.Hash.String(),
	}, nil
}

// countCommitsBetween counts commits reachable from `to` but not from
// `from` (exclusive of `from`). Walks `to`'s history, stopping when it
// hits the `from` commit. Returns 0 if from == to.
//
// go-git's iter.ForEach swallows storer.ErrStop and returns nil, mirroring
// the pattern in code_service_log.go (LogSince).
func countCommitsBetween(repo *gogit.Repository, from, to *object.Commit) (int, error) {
	if from.Hash == to.Hash {
		return 0, nil
	}
	iter, err := repo.Log(&gogit.LogOptions{From: to.Hash})
	if err != nil {
		return 0, err
	}
	defer iter.Close()
	n := 0
	err = iter.ForEach(func(c *object.Commit) error {
		if c.Hash == from.Hash {
			return storer.ErrStop
		}
		n++
		return nil
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}
