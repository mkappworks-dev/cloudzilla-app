package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// deleteGuard returns ErrForbidden if the caller is not an owner.
func deleteGuard(isOwner bool) error {
	if !isOwner {
		return fmt.Errorf("only the repo owner or org owner can delete a repo: %w", ErrForbidden)
	}
	return nil
}

func (s *RepoService) Delete(ctx context.Context, repoID, userID int64) error {
	repo, err := s.repos.GetByID(ctx, repoID)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if err := deleteGuard(s.IsOwner(ctx, repo, userID)); err != nil {
		return err
	}

	// deleted_at carries the suffix's second so Restore and purge find this
	// row's copy among other holders' copies of the name.
	now := time.Now()
	gitDir, _ := repoDirs(s.cfg.ReposRoot, repo.OwnerName, repo.Name)
	dirs := []string{gitDir}
	wikiDir, err := s.ownWikiDir(ctx, repo.OwnerName, repo.Name)
	if err != nil {
		return err
	}
	if wikiDir != "" {
		dirs = append(dirs, wikiDir)
	}
	moved, err := renameDirs(movesAside(deletedSuffix(now), dirs...))
	if err != nil {
		return fmt.Errorf("rename git dir for soft delete: %w", err)
	}
	if err := s.repos.Delete(ctx, repoID, repo.OwnerName, userID, now); err != nil {
		revertDirs(moved)
		return err
	}
	return nil
}

func (s *RepoService) Restore(ctx context.Context, repoID, requesterID int64, isSuperadmin bool) error {
	repo, err := s.repos.GetDeletedByID(ctx, repoID)
	if err != nil {
		return fmt.Errorf("deleted repo not found: %w", err)
	}
	if !isSuperadmin && !s.IsOwner(ctx, repo, requesterID) {
		return fmt.Errorf("forbidden: only the repo's owner or a superadmin can restore a repo")
	}
	if err := s.quota.CheckNewRepo(ctx, RepoQuotaOwner(repo)); err != nil {
		return err
	}

	// A soft-deleted org repo does not hold its name, so the name may have a new
	// holder even when this row's copy is gone; restoring beside it would share
	// its dirs.
	// The live wiki path is not checked: repos deleted before wikis moved with
	// them left theirs there, and renameDirs never overwrites one.
	gitDir, wikiDir := repoDirs(s.cfg.ReposRoot, repo.OwnerName, repo.Name)
	_, err = s.repos.GetByOwnerName(ctx, repo.OwnerName, repo.Name)
	switch {
	case err == nil, pathTaken(gitDir):
		return fmt.Errorf("restore conflict: %w", ErrRepoNameTaken)
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}
	suffix, ok := deletedCopySuffix(s.cfg.ReposRoot, *repo)
	if !ok {
		return fmt.Errorf("restore: no soft-deleted copy of %s/%s on disk", repo.OwnerName, repo.Name)
	}
	restored, err := renameDirs([]dirMove{{from: gitDir + suffix, to: gitDir}, {from: wikiDir + suffix, to: wikiDir}})
	if err != nil {
		return fmt.Errorf("rename git dir back on restore: %w", err)
	}

	if err := s.repos.Restore(ctx, repoID); err != nil {
		revertDirs(restored)
		return err
	}
	s.quota.Recompute(repo)
	return nil
}

func (s *RepoService) GetDeleted(ctx context.Context, ownerName, name string) (*model.Repository, error) {
	return s.repos.GetDeletedByOwnerAndName(ctx, ownerName, name)
}

func (s *RepoService) PurgeExpired(ctx context.Context) error {
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	expired, err := s.repos.PurgeExpired(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("purge expired repos: %w", err)
	}
	for _, r := range expired {
		removeDeletedCopy(s.cfg.ReposRoot, r)
		s.removeStrandedWiki(ctx, r.OwnerName, r.Name)
		if s.attachments != nil {
			if err := s.attachments.DeleteForRepo(ctx, r.ID); err != nil {
				slog.Warn("repo purge: delete attachments failed; the sweep will retry", "repo_id", r.ID, "error", err)
			}
		}
	}
	return nil
}
