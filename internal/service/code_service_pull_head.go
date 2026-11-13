package service

import (
	"fmt"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/revlist"
)

// PullHeadRef is the ref in the base repository that holds a cross-repository pull request's head commit.
func PullHeadRef(number int) plumbing.ReferenceName {
	return plumbing.ReferenceName(fmt.Sprintf("refs/pull/%d/head", number))
}

// CopyPullHead copies the commit a branch of the head repository points at, with every object the base lacks, into
// the base's object store and returns that commit. It writes no ref: the PR number is not known until the row exists.
func (s *CodeService) CopyPullHead(headOwner, headName, headBranch, baseOwner, baseName string) (plumbing.Hash, error) {
	head, err := s.openRepo(headOwner, headName)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("open head repository: %w", err)
	}
	base, err := s.openRepo(baseOwner, baseName)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("open base repository: %w", err)
	}
	tipRef, err := head.Reference(plumbing.NewBranchReferenceName(headBranch), true)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("head branch %q: %w", headBranch, err)
	}
	tip := tipRef.Hash()

	// The walk may only skip commits the head store can read; a base-only commit would fail it.
	var have []plumbing.Hash
	branches, err := base.Branches()
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("list base branches: %w", err)
	}
	_ = branches.ForEach(func(ref *plumbing.Reference) error {
		if head.Storer.HasEncodedObject(ref.Hash()) == nil {
			have = append(have, ref.Hash())
		}
		return nil
	})

	hashes, err := revlist.Objects(head.Storer, []plumbing.Hash{tip}, have)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("list head objects: %w", err)
	}
	for _, h := range hashes {
		if base.Storer.HasEncodedObject(h) == nil {
			continue
		}
		obj, err := head.Storer.EncodedObject(plumbing.AnyObject, h)
		if err != nil {
			return plumbing.ZeroHash, fmt.Errorf("read head object %s: %w", h, err)
		}
		if _, err := base.Storer.SetEncodedObject(obj); err != nil {
			return plumbing.ZeroHash, fmt.Errorf("write base object %s: %w", h, err)
		}
	}
	return tip, nil
}

// SetPullHeadRef points a pull request's head ref in the repository at commit.
func (s *CodeService) SetPullHeadRef(owner, repoName string, number int, commit plumbing.Hash) error {
	repo, err := s.openRepo(owner, repoName)
	if err != nil {
		return err
	}
	return setPullHeadRef(repo, number, commit)
}

func setPullHeadRef(repo *gogit.Repository, number int, commit plumbing.Hash) error {
	return repo.Storer.SetReference(plumbing.NewHashReference(PullHeadRef(number), commit))
}
