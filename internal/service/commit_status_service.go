package service

import (
	"context"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

type CommitStatusService struct {
	statuses *store.CommitStatusStore
	repos    *store.RepoStore
}

func NewCommitStatusService(statuses *store.CommitStatusStore, repos *store.RepoStore) *CommitStatusService {
	return &CommitStatusService{statuses: statuses, repos: repos}
}

func (s *CommitStatusService) Upsert(ctx context.Context, owner, repoName, sha string, cs *model.CommitStatus) error {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	cs.RepoID = repo.ID
	cs.SHA = sha
	if cs.Context == "" {
		cs.Context = "default"
	}
	return s.statuses.Upsert(ctx, cs)
}

func (s *CommitStatusService) List(ctx context.Context, owner, repoName, sha string) ([]model.CommitStatus, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	return s.statuses.ListBySHA(ctx, repo.ID, sha)
}

func (s *CommitStatusService) GetCombined(ctx context.Context, owner, repoName, sha string) (model.CommitStatusState, []model.CommitStatus, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return "", nil, fmt.Errorf("repo not found: %w", err)
	}
	statuses, err := s.statuses.ListBySHA(ctx, repo.ID, sha)
	if err != nil {
		return "", nil, err
	}
	combined, err := s.statuses.GetCombined(ctx, repo.ID, sha)
	if err != nil {
		return "", nil, err
	}
	return combined, statuses, nil
}
