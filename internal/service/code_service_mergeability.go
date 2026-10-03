package service

import (
	"context"
	"errors"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// Captures raw git-level state only; branch protection / required checks / reviews are layered on top by the PR handler.
type Mergeability struct {
	BaseRef      string
	HeadRef      string
	Ahead        int // commits in head not in base (excludes merge base)
	Behind       int // commits in base not in head (excludes merge base)
	HasConflicts bool
	MergeBase    string // SHA, empty if no common ancestor
}

func (s *CodeService) Mergeability(ctx context.Context, owner, repoName, base, head string) (Mergeability, error) {
	repo, err := s.openRepo(owner, repoName)
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
		if errors.Is(err, ErrNoCommonAncestor) {
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
	// Not mergeTreesNoConflict: checking mergeability must not write the merged tree into the repo.
	_, ok, err := mergeFiles(mb, baseCommit, headCommit)
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

// Counts commits reachable from `to` but not from `from`.
func countCommitsBetween(repo *gogit.Repository, from, to *object.Commit) (int, error) {
	commits, err := commitRange(repo, from.Hash, to.Hash)
	return len(commits), err
}
