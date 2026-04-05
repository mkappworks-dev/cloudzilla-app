package service

import (
	"context"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

// LabelService manages repository labels and their assignment to issues and PRs.
type LabelService struct {
	labels *store.LabelStore
	repos  *store.RepoStore
	issues *store.IssueStore
	pulls  *store.PullStore
}

// NewLabelService creates a LabelService backed by the given stores.
func NewLabelService(labels *store.LabelStore, repos *store.RepoStore, issues *store.IssueStore, pulls *store.PullStore) *LabelService {
	return &LabelService{labels: labels, repos: repos, issues: issues, pulls: pulls}
}

func (s *LabelService) Create(ctx context.Context, owner, repoName, name, color, description string) (*model.Label, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	label := &model.Label{
		RepoID:      repo.ID,
		Name:        name,
		Color:       color,
		Description: description,
	}
	if err := s.labels.Create(ctx, label); err != nil {
		return nil, err
	}
	return label, nil
}

func (s *LabelService) ListByRepo(ctx context.Context, owner, repoName string) ([]model.Label, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	return s.labels.ListByRepo(ctx, repo.ID)
}

func (s *LabelService) Delete(ctx context.Context, owner, repoName string, labelID int64) error {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	return s.labels.Delete(ctx, labelID, repo.ID)
}

func (s *LabelService) AddToIssue(ctx context.Context, owner, repoName string, issueNumber int, labelID int64) error {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	issue, err := s.issues.GetByNumberUnfiltered(ctx, repo.ID, issueNumber)
	if err != nil {
		return fmt.Errorf("issue not found: %w", err)
	}
	// Validate label belongs to repo
	if _, err := s.labels.GetByID(ctx, labelID, repo.ID); err != nil {
		return fmt.Errorf("label not found: %w", err)
	}
	return s.labels.AddToIssue(ctx, issue.ID, labelID)
}

func (s *LabelService) RemoveFromIssue(ctx context.Context, owner, repoName string, issueNumber int, labelID int64) error {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	issue, err := s.issues.GetByNumberUnfiltered(ctx, repo.ID, issueNumber)
	if err != nil {
		return fmt.Errorf("issue not found: %w", err)
	}
	return s.labels.RemoveFromIssue(ctx, issue.ID, labelID)
}

func (s *LabelService) GetForIssue(ctx context.Context, issueID int64) ([]model.Label, error) {
	return s.labels.ListByIssue(ctx, issueID)
}

func (s *LabelService) AddToPull(ctx context.Context, owner, repoName string, pullNumber int, labelID int64) error {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	pull, err := s.pulls.GetByNumber(ctx, repo.ID, pullNumber)
	if err != nil {
		return fmt.Errorf("pull not found: %w", err)
	}
	if _, err := s.labels.GetByID(ctx, labelID, repo.ID); err != nil {
		return fmt.Errorf("label not found: %w", err)
	}
	return s.labels.AddToPull(ctx, pull.ID, labelID)
}

func (s *LabelService) RemoveFromPull(ctx context.Context, owner, repoName string, pullNumber int, labelID int64) error {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	pull, err := s.pulls.GetByNumber(ctx, repo.ID, pullNumber)
	if err != nil {
		return fmt.Errorf("pull not found: %w", err)
	}
	return s.labels.RemoveFromPull(ctx, pull.ID, labelID)
}

func (s *LabelService) GetForPull(ctx context.Context, pullID int64) ([]model.Label, error) {
	return s.labels.ListByPull(ctx, pullID)
}

func (s *LabelService) BatchForIssues(ctx context.Context, issues []model.Issue) (map[int64][]model.Label, error) {
	ids := make([]int64, len(issues))
	for i, iss := range issues {
		ids[i] = iss.ID
	}
	return s.labels.ListByIssueIDs(ctx, ids)
}

func (s *LabelService) BatchForPulls(ctx context.Context, pulls []model.PullRequest) (map[int64][]model.Label, error) {
	ids := make([]int64, len(pulls))
	for i, p := range pulls {
		ids[i] = p.ID
	}
	return s.labels.ListByPullIDs(ctx, ids)
}
