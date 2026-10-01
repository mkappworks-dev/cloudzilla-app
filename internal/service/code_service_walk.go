package service

import (
	"context"

	"github.com/go-git/go-git/v5/plumbing/object"
)

// WalkTree takes the commit rather than a ref so a caller that keys on the commit hash walks exactly that commit.
func (s *CodeService) WalkTree(ctx context.Context, commit *object.Commit, fn func(path string, size int64) error) error {
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
