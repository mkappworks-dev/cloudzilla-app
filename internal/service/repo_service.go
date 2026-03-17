package service

import (
	"context"
	"fmt"
	"path/filepath"

	gogit "github.com/go-git/go-git/v5"
	"github.com/mkappworks/cloudzilla/internal/config"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

type RepoService struct {
	repos *store.RepoStore
	users *store.UserStore
	cfg   config.GitConfig
}

func NewRepoService(repos *store.RepoStore, users *store.UserStore, cfg config.GitConfig) *RepoService {
	return &RepoService{repos: repos, users: users, cfg: cfg}
}

func (s *RepoService) Create(ctx context.Context, ownerUsername, name, description string, private bool) (*model.Repository, error) {
	owner, err := s.users.GetByUsername(ctx, ownerUsername)
	if err != nil {
		return nil, fmt.Errorf("owner not found: %w", err)
	}

	r := &model.Repository{
		OwnerID:       owner.ID,
		OwnerName:     ownerUsername,
		Name:          name,
		Description:   description,
		Private:       private,
		DefaultBranch: "main",
	}
	if err := s.repos.CreateWithOwnerName(ctx, r); err != nil {
		return nil, err
	}

	repoPath := filepath.Join(s.cfg.ReposRoot, ownerUsername, name+".git")
	if _, err := gogit.PlainInit(repoPath, true); err != nil {
		return nil, fmt.Errorf("git init bare: %w", err)
	}

	return r, nil
}

func (s *RepoService) List(ctx context.Context) ([]model.Repository, error) {
	return s.repos.List(ctx)
}

func (s *RepoService) Get(ctx context.Context, owner, name string) (*model.Repository, error) {
	return s.repos.GetByOwnerName(ctx, owner, name)
}

func (s *RepoService) ListByOwner(ctx context.Context, ownerUsername string) ([]model.Repository, error) {
	return s.repos.GetByOwnerNameList(ctx, ownerUsername)
}

func (s *RepoService) CanRead(ctx context.Context, repo *model.Repository, userID *int64) bool {
	if !repo.Private {
		return true
	}

	if userID == nil {
		return false
	}

	if repo.OwnerID == *userID {
		return true
	}

	role, err := s.repos.GetPermission(ctx, repo.ID, *userID)
	if err != nil || role == "" {
		return false
	}

	return true
}

func (s *RepoService) CanWrite(ctx context.Context, repo *model.Repository, userID int64) bool {
	if repo.OwnerID == userID {
		return true
	}

	role, err := s.repos.GetPermission(ctx, repo.ID, userID)
	if err != nil || role == "" {
		return false
	}

	return role == string(model.RoleWriter) || role == string(model.RoleAdmin)
}
