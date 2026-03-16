package service

import (
	"context"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

type IssueService struct {
	issues *store.IssueStore
	repos  *store.RepoStore
}

func NewIssueService(issues *store.IssueStore, repos *store.RepoStore) *IssueService {
	return &IssueService{issues: issues, repos: repos}
}

func (s *IssueService) Create(ctx context.Context, owner, repoName string, authorID int64, title, body string) (*model.Issue, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	issue := &model.Issue{
		RepoID:   repo.ID,
		AuthorID: authorID,
		Title:    title,
		Body:     body,
		State:    model.IssueStateOpen,
	}
	if err := s.issues.Create(ctx, issue); err != nil {
		return nil, err
	}
	return issue, nil
}

func (s *IssueService) List(ctx context.Context, owner, repoName string) ([]model.Issue, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	return s.issues.List(ctx, repo.ID)
}

func (s *IssueService) Get(ctx context.Context, owner, repoName string, number int) (*model.Issue, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	return s.issues.GetByNumber(ctx, repo.ID, number)
}

func (s *IssueService) SetState(ctx context.Context, owner, repoName string, number int, state model.IssueState) (*model.Issue, error) {
	issue, err := s.Get(ctx, owner, repoName, number)
	if err != nil {
		return nil, err
	}
	if err := s.issues.UpdateState(ctx, issue.ID, state); err != nil {
		return nil, err
	}
	// Re-fetch so closed_at and updated_at reflect DB values
	return s.Get(ctx, owner, repoName, number)
}
