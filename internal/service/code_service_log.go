package service

import (
	"context"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

type LogSinceCommit struct {
	SHA         string
	AuthorEmail string
	AuthorName  string
	Time        time.Time
}

// Short-circuits as soon as a commit older than cutoff is seen.
func (s *CodeService) LogSince(ctx context.Context, owner, repoName, ref string, cutoff time.Time) ([]LogSinceCommit, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, err
	}
	// resolveRef treats "" as HEAD; the literal string "HEAD" would be tried as a branch/tag/SHA and fail.
	if ref == "HEAD" {
		ref = ""
	}
	head, _, err := resolveRef(repo, ref)
	if err != nil {
		return nil, err
	}
	iter, err := repo.Log(&gogit.LogOptions{From: head.Hash})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var out []LogSinceCommit
	err = iter.ForEach(func(c *object.Commit) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if c.Author.When.Before(cutoff) {
			return storer.ErrStop
		}
		out = append(out, LogSinceCommit{
			SHA:         c.Hash.String(),
			AuthorEmail: c.Author.Email,
			AuthorName:  c.Author.Name,
			Time:        c.Author.When,
		})
		return nil
	})
	if err != nil {
		return out, err
	}
	return out, nil
}
