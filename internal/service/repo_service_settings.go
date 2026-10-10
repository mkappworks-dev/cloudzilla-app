package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// archiveGuard returns ErrForbidden if the caller is not an owner.
func archiveGuard(isOwner bool) error {
	if !isOwner {
		return fmt.Errorf("only the repo owner or org owner can archive a repo: %w", ErrForbidden)
	}
	return nil
}

// templateGuard returns ErrForbidden if the caller is not an owner.
func templateGuard(isOwner bool) error {
	if !isOwner {
		return fmt.Errorf("only the repo owner or org owner can change template status: %w", ErrForbidden)
	}
	return nil
}

func (s *RepoService) Archive(ctx context.Context, repoID, userID int64) error {
	repo, err := s.repos.GetByID(ctx, repoID)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if err := archiveGuard(s.IsOwner(ctx, repo, userID)); err != nil {
		return err
	}
	return s.repos.SetArchived(ctx, repoID, true)
}

func (s *RepoService) Unarchive(ctx context.Context, repoID, userID int64) error {
	repo, err := s.repos.GetByID(ctx, repoID)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if err := archiveGuard(s.IsOwner(ctx, repo, userID)); err != nil {
		return err
	}
	return s.repos.SetArchived(ctx, repoID, false)
}

func (s *RepoService) SetTemplate(ctx context.Context, repoID, userID int64, isTemplate bool) error {
	repo, err := s.repos.GetByID(ctx, repoID)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if err := templateGuard(s.IsOwner(ctx, repo, userID)); err != nil {
		return err
	}
	return s.repos.SetTemplate(ctx, repoID, isTemplate)
}

// UpdateMeta updates the user-editable repository metadata (description,
// website, license). Requires manage permission on the repo.
func (s *RepoService) UpdateMeta(ctx context.Context, repoID, userID int64, description, website, license string) error {
	repo, err := s.repos.GetByID(ctx, repoID)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if !s.CanManage(ctx, repo, userID) {
		return fmt.Errorf("manage permission required: %w", ErrForbidden)
	}
	return s.repos.UpdateMeta(ctx, repoID, strings.TrimSpace(description), strings.TrimSpace(website), strings.TrimSpace(license))
}

// UpdateGeneral updates the description, website, and default branch, and
// points the bare repo's HEAD at that branch. Requires manage permission; an
// empty defaultBranch keeps the current one. The branch must exist unless the
// repo has no branches yet.
func (s *RepoService) UpdateGeneral(ctx context.Context, repoID, userID int64, description, website, defaultBranch string) error {
	repo, err := s.repos.GetByID(ctx, repoID)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if !s.CanManage(ctx, repo, userID) {
		return fmt.Errorf("manage permission required: %w", ErrForbidden)
	}
	branch := strings.TrimSpace(defaultBranch)
	if branch == "" {
		branch = repo.DefaultBranch
	}
	if branch != repo.DefaultBranch {
		if err := CheckContentWritable(repo); err != nil {
			return err
		}
	}
	branchRef := plumbing.NewBranchReferenceName(branch)
	if err := branchRef.Validate(); err != nil {
		return fmt.Errorf("%w: %q is not a valid branch name", ErrInvalidDefaultBranch, branch)
	}

	gitDir, _ := repoDirs(s.cfg.ReposRoot, repo.OwnerName, repo.Name)
	bare, err := gogit.PlainOpen(gitDir)
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}
	if err := requireBranchUnlessEmpty(bare, branchRef); err != nil {
		return err
	}
	oldHead, err := bare.Storer.Reference(plumbing.HEAD)
	if err != nil {
		return fmt.Errorf("read HEAD: %w", err)
	}
	// Rewritten even when unchanged so a save repairs a HEAD that drifted from the column.
	if err := bare.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, branchRef)); err != nil {
		return fmt.Errorf("set HEAD: %w", err)
	}
	if err := s.repos.UpdateGeneral(ctx, repoID, strings.TrimSpace(description), strings.TrimSpace(website), branch); err != nil {
		if rbErr := bare.Storer.SetReference(oldHead); rbErr != nil {
			slog.Error("restore HEAD after failed settings update", "repo_id", repoID, "error", rbErr)
		}
		return err
	}
	return nil
}

// requireBranchUnlessEmpty refuses a branch the repo doesn't have, except in a
// repo with no branches, where the first push creates it.
func requireBranchUnlessEmpty(bare *gogit.Repository, branchRef plumbing.ReferenceName) error {
	_, err := bare.Storer.Reference(branchRef)
	if !errors.Is(err, plumbing.ErrReferenceNotFound) {
		return err
	}
	branches, err := bare.Branches()
	if err != nil {
		return fmt.Errorf("list branches: %w", err)
	}
	defer branches.Close()
	_, err = branches.Next()
	switch {
	case errors.Is(err, io.EOF):
		return nil
	case err != nil:
		return fmt.Errorf("list branches: %w", err)
	}
	return fmt.Errorf("%w: %q is not a branch in this repository", ErrInvalidDefaultBranch, branchRef.Short())
}

// UpdateFeatureToggles updates the Issues/Discussions/Projects/Wiki feature
// flags. Requires manage permission.
func (s *RepoService) UpdateFeatureToggles(ctx context.Context, repoID, userID int64, issues, discussions, projects, wiki bool) error {
	repo, err := s.repos.GetByID(ctx, repoID)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if !s.CanManage(ctx, repo, userID) {
		return fmt.Errorf("manage permission required: %w", ErrForbidden)
	}
	return s.repos.UpdateFeatureToggles(ctx, repoID, issues, discussions, projects, wiki)
}

func (s *RepoService) UpdateVisibility(ctx context.Context, repoID, userID int64, private bool) error {
	repo, err := s.repos.GetByID(ctx, repoID)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if !s.CanManage(ctx, repo, userID) {
		return fmt.Errorf("manage permission required: %w", ErrForbidden)
	}
	return s.repos.UpdateVisibility(ctx, repoID, private)
}
