package service

import (
	"context"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

type PullService struct {
	pulls *store.PullStore
	repos *store.RepoStore
}

func NewPullService(pulls *store.PullStore, repos *store.RepoStore) *PullService {
	return &PullService{pulls: pulls, repos: repos}
}

func (s *PullService) Create(ctx context.Context, owner, repoName string, authorID int64, title, body, head, base string, isDraft bool) (*model.PullRequest, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	pr := &model.PullRequest{
		RepoID:     repo.ID,
		AuthorID:   authorID,
		Title:      title,
		Body:       body,
		State:      model.PRStateOpen,
		HeadBranch: head,
		BaseBranch: base,
		IsDraft:    isDraft,
	}
	if err := s.pulls.Create(ctx, pr); err != nil {
		return nil, err
	}
	return pr, nil
}

func (s *PullService) List(ctx context.Context, owner, repoName string) ([]model.PullRequest, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	return s.pulls.List(ctx, repo.ID)
}

func (s *PullService) Get(ctx context.Context, owner, repoName string, number int) (*model.PullRequest, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	return s.pulls.GetByNumber(ctx, repo.ID, number)
}

func (s *PullService) SetDraft(ctx context.Context, owner, repoName string, number int, isDraft bool) error {
	pr, err := s.Get(ctx, owner, repoName, number)
	if err != nil {
		return err
	}
	if pr.State == model.PRStateMerged || pr.State == model.PRStateClosed {
		return fmt.Errorf("cannot change draft state of a closed or merged pull request")
	}
	return s.pulls.SetDraft(ctx, pr.ID, isDraft)
}

func (s *PullService) SetState(ctx context.Context, owner, repoName string, number int, state model.PRState) (*model.PullRequest, error) {
	pr, err := s.Get(ctx, owner, repoName, number)
	if err != nil {
		return nil, err
	}
	if pr.State == model.PRStateMerged {
		return nil, fmt.Errorf("merged PRs cannot be updated")
	}
	if err := s.pulls.UpdateState(ctx, pr.ID, state); err != nil {
		return nil, err
	}
	// Re-fetch so merged_at/closed_at and updated_at reflect DB values
	return s.Get(ctx, owner, repoName, number)
}
