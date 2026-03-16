package store

import (
	"context"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store/db"
)

type RepoStore struct{ q *db.Queries }

func NewRepoStore(q *db.Queries) *RepoStore { return &RepoStore{q: q} }

func (s *RepoStore) Create(ctx context.Context, r *model.Repository) error {
	result, err := s.q.CreateRepo(ctx, db.CreateRepoParams{
		OwnerID:       r.OwnerID,
		Name:          r.Name,
		Description:   r.Description,
		Private:       r.Private,
		DefaultBranch: r.DefaultBranch,
	})
	if err != nil {
		return fmt.Errorf("repo create: %w", err)
	}
	r.ID = result.ID
	r.CreatedAt = result.CreatedAt
	r.UpdatedAt = result.UpdatedAt
	return nil
}

func (s *RepoStore) List(ctx context.Context) ([]model.Repository, error) {
	repos, err := s.q.ListRepos(ctx)
	if err != nil {
		return nil, fmt.Errorf("repo list: %w", err)
	}
	return mapDBReposToModel(repos), nil
}

func (s *RepoStore) GetByOwnerAndName(ctx context.Context, ownerName, name string) (*model.Repository, error) {
	result, err := s.q.GetRepoByOwnerAndName(ctx, db.GetRepoByOwnerAndNameParams{
		Username: ownerName,
		Name:     name,
	})
	if err != nil {
		return nil, fmt.Errorf("repo get: %w", err)
	}
	return mapDBRepoToModel(&result), nil
}

func (s *RepoStore) GetByOwnerID(ctx context.Context, ownerID int64) ([]model.Repository, error) {
	repos, err := s.q.GetReposByOwnerID(ctx, ownerID)
	if err != nil {
		return nil, fmt.Errorf("repo list by owner: %w", err)
	}
	return mapDBReposToModel(repos), nil
}

func mapDBRepoToModel(dbRepo *db.Repository) *model.Repository {
	return &model.Repository{
		ID:            dbRepo.ID,
		OwnerID:       dbRepo.OwnerID,
		Name:          dbRepo.Name,
		Description:   dbRepo.Description,
		Private:       dbRepo.Private,
		DefaultBranch: dbRepo.DefaultBranch,
		CreatedAt:     dbRepo.CreatedAt,
		UpdatedAt:     dbRepo.UpdatedAt,
	}
}

func mapDBReposToModel(dbRepos []db.Repository) []model.Repository {
	repos := make([]model.Repository, len(dbRepos))
	for i, dbRepo := range dbRepos {
		repos[i] = *mapDBRepoToModel(&dbRepo)
	}
	return repos
}
