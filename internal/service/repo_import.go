package service

import (
	"context"
	"fmt"
	"os"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// CheckImportName fails fast before a long clone; claimRepo checks again at publish.
func (s *RepoService) CheckImportName(ctx context.Context, ownerName, name string) error {
	if err := ValidateRepoName(name); err != nil {
		return fmt.Errorf("invalid repository name: %w", err)
	}
	held, err := s.repos.NameHeld(ctx, ownerName, name)
	if err != nil {
		return err
	}
	if held {
		return ErrRepoNameTaken
	}
	return nil
}

// CreateFromImport adopts srcDir, a bare repo an import cloned, as t's repo
// name. srcDir is left in place on failure; the caller removes it.
func (s *RepoService) CreateFromImport(ctx context.Context, t RepoTarget, name, description string, private bool, defaultBranch, srcDir string) (*model.Repository, error) {
	return s.createFromImport(ctx, t, name, description, private, defaultBranch, srcDir, nil)
}

// createFromImport runs then once the repo's row exists; its failure undoes
// the whole import, so a mirror never appears without its mirror row.
func (s *RepoService) createFromImport(ctx context.Context, t RepoTarget, name, description string, private bool, defaultBranch, srcDir string,
	then func(context.Context, *model.Repository) error) (*model.Repository, error) {
	if err := ValidateRepoName(name); err != nil {
		return nil, fmt.Errorf("invalid repository name: %w", err)
	}
	if t.OrgID != 0 {
		if !s.isOrgOwner(ctx, t.OrgID, t.ActorID) {
			return nil, fmt.Errorf("no longer an owner of %s: %w", t.OwnerName, ErrForbidden)
		}
	} else if _, err := s.personalOwner(ctx, t.OwnerID, t.OwnerName); err != nil {
		return nil, err
	}

	if err := s.CheckNewRepoQuota(ctx, t); err != nil {
		return nil, err
	}

	gitDir, err := claimRepo(ctx, s.repos, s.cfg.ReposRoot, t.OwnerName, name)
	if err != nil {
		return nil, err
	}
	r := &model.Repository{
		OwnerID:       t.OwnerID,
		OrgID:         t.OrgID,
		CreatedBy:     t.ActorID,
		OwnerName:     t.OwnerName,
		Name:          name,
		Description:   description,
		Private:       private,
		DefaultBranch: defaultBranch,
	}
	if err := s.repos.CreateWithOwnerName(ctx, r); err != nil {
		abandonNewRepo(ctx, s.repos, 0, gitDir)
		return nil, repoNameErr("create imported repo", err)
	}
	if then != nil {
		if err := then(ctx, r); err != nil {
			abandonNewRepo(ctx, s.repos, r.ID, gitDir)
			return nil, err
		}
	}
	// os.Rename won't replace a directory, so the empty claim goes first. The
	// row already holds the name, so a concurrent create still sees it taken.
	if err := os.Remove(gitDir); err != nil {
		abandonNewRepo(ctx, s.repos, r.ID, gitDir)
		return nil, fmt.Errorf("remove claimed dir: %w", err)
	}
	if err := os.Rename(srcDir, gitDir); err != nil {
		abandonNewRepo(ctx, s.repos, r.ID, gitDir)
		return nil, fmt.Errorf("move imported repo into place: %w", err)
	}
	s.quota.Recompute(r)
	return r, nil
}
