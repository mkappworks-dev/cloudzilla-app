package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

type WatchService struct {
	watches *store.WatchStore
	repos   *store.RepoStore
}

func NewWatchService(watches *store.WatchStore, repos *store.RepoStore) *WatchService {
	return &WatchService{watches: watches, repos: repos}
}

// Watch sets (or updates) the watch level for the given user on the repo identified by owner/repoName.
func (s *WatchService) Watch(ctx context.Context, owner, repoName string, userID int64, level string) error {
	if level != model.WatchLevelWatching && level != model.WatchLevelReleasesOnly && level != model.WatchLevelIgnoring {
		return fmt.Errorf("invalid watch level: %s", level)
	}
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	return s.watches.Set(ctx, userID, repo.ID, level)
}

// Unwatch removes the watch row for the given user on the repo.
func (s *WatchService) Unwatch(ctx context.Context, owner, repoName string, userID int64) error {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	return s.watches.Delete(ctx, userID, repo.ID)
}

// GetLevel returns the watch level for the given user on repoID.
// Returns "" if the user is not watching. DB errors are logged and treated as not-watching.
func (s *WatchService) GetLevel(ctx context.Context, userID, repoID int64) string {
	w, err := s.watches.Get(ctx, userID, repoID)
	if err != nil {
		slog.Error("WatchService.GetLevel: DB error", "user_id", userID, "repo_id", repoID, "error", err)
		return ""
	}
	if w == nil {
		return ""
	}
	return w.Level
}
