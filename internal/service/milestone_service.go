package service

import (
	"context"
	"fmt"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// MilestoneService manages milestone creation, updates, and issue/PR associations.
type MilestoneService struct {
	milestones *store.MilestoneStore
	repos      *store.RepoStore
}

// NewMilestoneService creates a MilestoneService backed by the given stores.
func NewMilestoneService(milestones *store.MilestoneStore, repos *store.RepoStore) *MilestoneService {
	return &MilestoneService{milestones: milestones, repos: repos}
}

func (s *MilestoneService) Create(ctx context.Context, owner, repoName, title, description string, dueDate *time.Time) (*model.Milestone, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	m := &model.Milestone{
		RepoID:      repo.ID,
		Title:       title,
		Description: description,
		State:       "open",
		DueDate:     dueDate,
	}
	if err := s.milestones.Create(ctx, m); err != nil {
		return nil, fmt.Errorf("create milestone: %w", err)
	}
	return m, nil
}

func (s *MilestoneService) ListByRepo(ctx context.Context, owner, repoName string) ([]model.Milestone, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	return s.milestones.ListByRepo(ctx, repo.ID)
}

func (s *MilestoneService) GetByNumber(ctx context.Context, owner, repoName string, number int) (*model.Milestone, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	return s.milestones.GetByNumber(ctx, repo.ID, number)
}

func (s *MilestoneService) GetByID(ctx context.Context, id int64) (*model.Milestone, error) {
	return s.milestones.GetByID(ctx, id)
}

func (s *MilestoneService) Update(ctx context.Context, owner, repoName string, number int, title, description string, dueDate *time.Time) (*model.Milestone, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	m, err := s.milestones.GetByNumber(ctx, repo.ID, number)
	if err != nil {
		return nil, fmt.Errorf("milestone not found: %w", err)
	}
	m.Title = title
	m.Description = description
	m.DueDate = dueDate
	if err := s.milestones.Update(ctx, m); err != nil {
		return nil, fmt.Errorf("update milestone: %w", err)
	}
	return m, nil
}

func (s *MilestoneService) Close(ctx context.Context, owner, repoName string, number int) (*model.Milestone, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	m, err := s.milestones.GetByNumber(ctx, repo.ID, number)
	if err != nil {
		return nil, fmt.Errorf("milestone not found: %w", err)
	}
	now := time.Now().UTC()
	m.State = "closed"
	m.ClosedAt = &now
	if err := s.milestones.Update(ctx, m); err != nil {
		return nil, fmt.Errorf("close milestone: %w", err)
	}
	return m, nil
}

func (s *MilestoneService) Reopen(ctx context.Context, owner, repoName string, number int) (*model.Milestone, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	m, err := s.milestones.GetByNumber(ctx, repo.ID, number)
	if err != nil {
		return nil, fmt.Errorf("milestone not found: %w", err)
	}
	m.State = "open"
	m.ClosedAt = nil
	if err := s.milestones.Update(ctx, m); err != nil {
		return nil, fmt.Errorf("reopen milestone: %w", err)
	}
	return m, nil
}

func (s *MilestoneService) Delete(ctx context.Context, owner, repoName string, number int) error {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	return s.milestones.Delete(ctx, repo.ID, number)
}

// SetIssue assigns or removes a milestone from an issue.
func (s *MilestoneService) SetIssue(ctx context.Context, issueID int64, milestoneID *int64) error {
	return s.milestones.SetIssue(ctx, issueID, milestoneID)
}

// SetPull assigns or removes a milestone from a pull request.
func (s *MilestoneService) SetPull(ctx context.Context, pullID int64, milestoneID *int64) error {
	return s.milestones.SetPull(ctx, pullID, milestoneID)
}

// GetForIssue returns the milestone attached to an issue (nil if none).
func (s *MilestoneService) GetForIssue(ctx context.Context, issueID int64) (*model.Milestone, error) {
	mid, err := s.milestones.GetIssueID(ctx, issueID)
	if err != nil || mid == nil {
		return nil, err
	}
	return s.milestones.GetByID(ctx, *mid)
}

// GetForPull returns the milestone attached to a pull request (nil if none).
func (s *MilestoneService) GetForPull(ctx context.Context, pullID int64) (*model.Milestone, error) {
	mid, err := s.milestones.GetPullID(ctx, pullID)
	if err != nil || mid == nil {
		return nil, err
	}
	return s.milestones.GetByID(ctx, *mid)
}

// ListIssues returns a page of issues assigned to the milestone, filtered by state.
func (s *MilestoneService) ListIssues(ctx context.Context, milestoneID int64, state string, page, pageSize int) ([]model.Issue, error) {
	return s.milestones.ListIssuesPaged(ctx, milestoneID, state, page, pageSize)
}

// ListPulls returns a page of pull requests assigned to the milestone, filtered by state.
func (s *MilestoneService) ListPulls(ctx context.Context, milestoneID int64, state string, page, pageSize int) ([]model.PullRequest, error) {
	return s.milestones.ListPullsPaged(ctx, milestoneID, state, page, pageSize)
}

// PullCounts returns the open and closed pull-request counts for the milestone.
func (s *MilestoneService) PullCounts(ctx context.Context, milestoneID int64) (open, closed int, err error) {
	return s.milestones.PullCounts(ctx, milestoneID)
}
