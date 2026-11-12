package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

var (
	ErrRepoNameUnchanged = errors.New("the repository already has that name")
	ErrRepoSyncing       = errors.New("a mirror sync is running; rename the repository when it finishes")
)

// RenameRepo gives repo, as seen by a requester who can manage it, the name
// newName. Both dirs and the row move together; the old path stays reachable
// as an alias. Errors: ErrForbidden, an ErrInvalidRepoName or ErrRepoNameReserved
// (422), ErrRepoNameUnchanged (422), ErrRepoSyncing and ErrRepoNameTaken (409).
func (s *RepoService) RenameRepo(ctx context.Context, repo *model.Repository, requesterID int64, newName string) error {
	if !s.CanManage(ctx, repo, requesterID) {
		return fmt.Errorf("only an admin of the repository can rename it: %w", ErrForbidden)
	}
	if err := ValidateRepoName(newName); err != nil {
		return err
	}
	if newName == repo.Name {
		return ErrRepoNameUnchanged
	}
	if err := s.checkNotSyncing(ctx, repo.ID); err != nil {
		return err
	}
	// A case-only rename finds the repo's own row.
	if !strings.EqualFold(newName, repo.Name) {
		if held, err := s.repos.NameHeld(ctx, repo.OwnerName, newName); err != nil {
			return err
		} else if held {
			return ErrRepoNameTaken
		}
	}
	return s.moveRepo(ctx, repo, repo.OwnerName, newName, func() error {
		return s.repos.Rename(ctx, repo.ID, repo.OwnerName, repo.Name, newName)
	})
}

// checkNotSyncing is a point-in-time check: a sync that claims the lease right
// after it fails on its stale dir, as a push that loses the race does.
func (s *RepoService) checkNotSyncing(ctx context.Context, repoID int64) error {
	if s.mirrors == nil {
		return nil
	}
	leased, err := s.mirrors.Leased(ctx, repoID)
	if err != nil {
		return err
	}
	if leased {
		return ErrRepoSyncing
	}
	return nil
}
