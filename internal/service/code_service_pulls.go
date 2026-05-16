package service

import (
	"strings"

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
	_ = iterBase.ForEach(func(c *object.Commit) error {
		excluded[c.Hash] = true
		return nil
	})
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
		hash := c.Hash.String()
		shortHash := hash
		if len(hash) > 7 {
			shortHash = hash[:7]
		}
		msg := strings.TrimSpace(c.Message)
		firstLine := msg
		if idx := strings.Index(msg, "\n"); idx >= 0 {
			firstLine = strings.TrimSpace(msg[:idx])
		}
		out = append(out, CommitSummary{
			Hash:       shortHash,
			FullHash:   hash,
			Message:    firstLine,
			Author:     c.Author.Name,
			AuthorTime: c.Author.When,
		})
		return nil
	})
	return out, err
}
