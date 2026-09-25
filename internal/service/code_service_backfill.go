package service

import (
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type WalkedCommit struct {
	SHA         string
	AuthorEmail string
	When        time.Time
	Additions   int
	Deletions   int
}

// Walks every branch, not just the default, so stats match what post-receive ingests for pushes to any branch.
func (s *CodeService) WalkAllRefCommits(owner, repoName string) ([]WalkedCommit, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, err
	}
	branches, err := repo.Branches()
	if err != nil {
		return nil, err
	}
	seen := make(map[plumbing.Hash]struct{})
	var out []WalkedCommit
	err = branches.ForEach(func(ref *plumbing.Reference) error {
		iter, err := repo.Log(&gogit.LogOptions{From: ref.Hash()})
		if err != nil {
			return err
		}
		defer iter.Close()
		return iter.ForEach(func(c *object.Commit) error {
			if _, dup := seen[c.Hash]; dup {
				return nil
			}
			seen[c.Hash] = struct{}{}
			stats, err := c.Stats()
			if err != nil {
				return err
			}
			add, del := 0, 0
			for _, fs := range stats {
				add += fs.Addition
				del += fs.Deletion
			}
			out = append(out, WalkedCommit{
				SHA:         c.Hash.String(),
				AuthorEmail: c.Author.Email,
				When:        c.Author.When,
				Additions:   add,
				Deletions:   del,
			})
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
