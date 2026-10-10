package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// ErrForkIntoSourceOwner refuses a fork into the namespace that holds the
// source: that would be a plain copy, which GitHub refuses too.
var ErrForkIntoSourceOwner = errors.New("a repository can't be forked into the account or organization that owns it")

// ErrForkDefaultBranchMissing refuses a default-branch-only fork whose source
// names a default branch that isn't one of its branches; settings accept any string.
var ErrForkDefaultBranchMissing = errors.New("the default branch doesn't exist, so it can't be the only branch copied")

type ForkOptions struct {
	Owner             string  // "" = the actor's account
	Name              string  // "" = the source's name, suffixed -1, -2, … while taken
	Description       *string // nil = the source's description
	DefaultBranchOnly bool
}

func (s *RepoService) Fork(ctx context.Context, originalOwner, originalName string, actorID int64, actorUsername string, opts ForkOptions) (*model.Repository, error) {
	orig, err := s.repos.GetByOwnerName(ctx, originalOwner, originalName)
	if err != nil {
		return nil, fmt.Errorf("original repo not found: %w", err)
	}

	if !s.CanRead(ctx, orig, &actorID) {
		return nil, fmt.Errorf("access denied")
	}
	target, err := s.ResolveRepoTarget(ctx, actorID, actorUsername, opts.Owner)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(target.OwnerName, orig.OwnerName) {
		return nil, ErrForkIntoSourceOwner
	}
	if err := s.CheckNewRepoQuota(ctx, target); err != nil {
		return nil, err
	}

	forkName, dstPath, err := s.claimForkName(ctx, target.OwnerName, originalName, opts.Name)
	if err != nil {
		return nil, err
	}

	description := orig.Description
	if opts.Description != nil {
		description = *opts.Description
	}
	forked := &model.Repository{
		OwnerID:       target.OwnerID,
		OrgID:         target.OrgID,
		CreatedBy:     actorID,
		OwnerName:     target.OwnerName,
		Name:          forkName,
		Description:   description,
		Private:       orig.Private,
		DefaultBranch: orig.DefaultBranch,
		ForkOfID:      &orig.ID,
	}
	if err := s.repos.Fork(ctx, forked); err != nil {
		abandonNewRepo(ctx, s.repos, 0, dstPath)
		return nil, repoNameErr("fork db record", err)
	}

	srcPath, _ := repoDirs(s.cfg.ReposRoot, originalOwner, originalName)
	if err := copyDir(srcPath, dstPath); err != nil {
		abandonNewRepo(ctx, s.repos, forked.ID, dstPath)
		return nil, fmt.Errorf("copy git dir: %w", err)
	}

	if opts.DefaultBranchOnly {
		if err := pruneToDefaultBranch(dstPath, orig.DefaultBranch); err != nil {
			abandonNewRepo(ctx, s.repos, forked.ID, dstPath)
			if errors.Is(err, ErrForkDefaultBranchMissing) {
				return nil, err
			}
			return nil, fmt.Errorf("prune fork branches: %w", err)
		}
	}

	_ = s.repos.IncrementForkCount(ctx, orig.ID)

	forked.ForkOfOwner = originalOwner
	forked.ForkOfName = originalName
	s.quota.Recompute(forked)
	return forked, nil
}

// claimForkName claims name in owner, or, when name is "", the first free
// one of base, base-1, base-2, …
func (s *RepoService) claimForkName(ctx context.Context, owner, base, name string) (string, string, error) {
	if name != "" {
		if err := ValidateRepoName(name); err != nil {
			return "", "", fmt.Errorf("invalid repository name: %w", err)
		}
		dstPath, err := claimRepo(ctx, s.repos, s.cfg.ReposRoot, owner, name)
		return name, dstPath, err
	}
	for i := 0; ; i++ {
		name = base
		if i > 0 {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		dstPath, err := claimRepo(ctx, s.repos, s.cfg.ReposRoot, owner, name)
		if !errors.Is(err, ErrRepoNameTaken) && !errors.Is(err, ErrRepoNameReserved) {
			return name, dstPath, err
		}
	}
}

// ForksOwnedBy lists repoID's forks in userID's account and the orgs they own.
func (s *RepoService) ForksOwnedBy(ctx context.Context, repoID, userID int64) ([]model.Repository, error) {
	return s.repos.ListForksOwnedBy(ctx, repoID, userID)
}

// copyDir recursively copies src directory to dst.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// pruneToDefaultBranch drops every branch of the bare repo at gitDir except
// defaultBranch and points HEAD at it. The dropped branches' objects stay:
// there's no pure-Go gc, and a full fork holds them anyway. A repo with no
// branches has nothing to prune and is left alone.
func pruneToDefaultBranch(gitDir, defaultBranch string) error {
	repo, err := gogit.PlainOpen(gitDir)
	if err != nil {
		return err
	}
	refs, err := repo.References()
	if err != nil {
		return err
	}
	keep := plumbing.NewBranchReferenceName(defaultBranch)
	var drop []plumbing.ReferenceName
	hasKeep := false
	if err := refs.ForEach(func(r *plumbing.Reference) error {
		switch {
		case r.Name() == keep:
			hasKeep = true
		case r.Name().IsBranch():
			drop = append(drop, r.Name())
		}
		return nil
	}); err != nil {
		return err
	}
	if !hasKeep {
		if len(drop) == 0 {
			return nil
		}
		return ErrForkDefaultBranchMissing
	}
	for _, name := range drop {
		if err := repo.Storer.RemoveReference(name); err != nil {
			return err
		}
	}
	return repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, keep))
}
