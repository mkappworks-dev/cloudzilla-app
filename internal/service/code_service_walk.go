package service

import (
	"context"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func (s *CodeService) WalkTree(ctx context.Context, owner, repoName, ref string, fn func(path string, size int64) error) error {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return err
	}
	// resolveRef treats "" as HEAD; the literal string "HEAD" would be tried as a branch/tag/SHA and fail.
	if ref == "HEAD" {
		ref = ""
	}
	commit, _, err := resolveRef(repo, ref)
	if err != nil {
		return err
	}
	tree, err := commit.Tree()
	if err != nil {
		return err
	}
	return tree.Files().ForEach(func(f *object.File) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fn(f.Name, f.Size)
	})
}
