package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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
		Name:          name,
		Description:   description,
		Private:       private,
		DefaultBranch: "main",
	}
	if err := s.repos.Create(ctx, r); err != nil {
		return nil, err
	}

	// Create bare git repo directory
	repoPath := filepath.Join(s.cfg.ReposRoot, ownerUsername, name+".git")
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		return nil, fmt.Errorf("create repo dir: %w", err)
	}

	r.OwnerName = ownerUsername
	return r, nil
}

func (s *RepoService) List(ctx context.Context) ([]model.Repository, error) {
	return s.repos.List(ctx)
}

func (s *RepoService) Get(ctx context.Context, owner, name string) (*model.Repository, error) {
	return s.repos.GetByOwnerAndName(ctx, owner, name)
}

func (s *RepoService) ListByOwner(ctx context.Context, ownerUsername string) ([]model.Repository, error) {
	owner, err := s.users.GetByUsername(ctx, ownerUsername)
	if err != nil {
		return nil, fmt.Errorf("owner not found: %w", err)
	}
	return s.repos.GetByOwnerID(ctx, owner.ID)
}
