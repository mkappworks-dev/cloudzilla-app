package service

import (
	"context"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

// StarService manages repository starring and unstarring.
type StarService struct {
	stars *store.StarStore
	repos *store.RepoStore
	users *store.UserStore
}

// NewStarService creates a StarService backed by the given stores.
func NewStarService(stars *store.StarStore, repos *store.RepoStore, users *store.UserStore) *StarService {
	return &StarService{stars: stars, repos: repos, users: users}
}

func (s *StarService) Star(ctx context.Context, owner, repoName string, userID int64) error {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	return s.stars.Star(ctx, userID, repo.ID)
}

func (s *StarService) Unstar(ctx context.Context, owner, repoName string, userID int64) error {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	return s.stars.Unstar(ctx, userID, repo.ID)
}

func (s *StarService) GetStarCount(ctx context.Context, repoID int64) (int, error) {
	return s.stars.CountByRepo(ctx, repoID)
}

func (s *StarService) IsStarred(ctx context.Context, repoID, userID int64) (bool, error) {
	return s.stars.IsStarred(ctx, userID, repoID)
}

func (s *StarService) ListStargazers(ctx context.Context, owner, repoName string) ([]model.User, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	return s.stars.ListStargazers(ctx, repo.ID)
}

func (s *StarService) ListByUser(ctx context.Context, username string) ([]model.Repository, error) {
	user, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}
	return s.stars.ListByUser(ctx, user.ID)
}
