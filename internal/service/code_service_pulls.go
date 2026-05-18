package service

import (
	"fmt"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// PullCommits returns commits reachable from head but not from base, in
// reverse-chronological order.
func (s *CodeService) PullCommits(owner, repoName, base, head string) ([]CommitSummary, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, err
	}
	baseCommit, _, err := resolveRef(repo, base)
	if err != nil {
		return nil, err
	}
	headCommit, _, err := resolveRef(repo, head)
	if err != nil {
		return nil, err
	}

	excluded := make(map[plumbing.Hash]bool)
	iterBase, err := repo.Log(&gogit.LogOptions{From: baseCommit.Hash})
	if err != nil {
		return nil, err
	}
	if walkErr := iterBase.ForEach(func(c *object.Commit) error {
		excluded[c.Hash] = true
		return nil
	}); walkErr != nil {
		iterBase.Close()
		return nil, fmt.Errorf("walk base history: %w", walkErr)
	}
	iterBase.Close()

	iter, err := repo.Log(&gogit.LogOptions{From: headCommit.Hash, Order: gogit.LogOrderCommitterTime})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var out []CommitSummary
	err = iter.ForEach(func(c *object.Commit) error {
		if excluded[c.Hash] {
			return nil
		}
		out = append(out, summarizeCommit(c))
		return nil
	})
	return out, err
}
