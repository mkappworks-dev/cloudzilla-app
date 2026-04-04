package service

import (
	"context"
	"fmt"
	"time"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

// pinLimitGuard returns an error string if pinnedCount is at or above the 3-pin limit.
// An empty string means no error.
func pinLimitGuard(pinnedCount int) string {
	if pinnedCount >= 3 {
		return "repositories may not have more than 3 pinned issues"
	}
	return ""
}

type IssueService struct {
	issues   *store.IssueStore
	repos    *store.RepoStore
	repoSvc  *RepoService
}

func NewIssueService(issues *store.IssueStore, repos *store.RepoStore, repoSvc *RepoService) *IssueService {
	return &IssueService{issues: issues, repos: repos, repoSvc: repoSvc}
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

// PinIssue pins an issue. Requires CanManage. Maximum 3 pinned issues per repo.
func (s *IssueService) PinIssue(ctx context.Context, owner, repoName string, number int, userID int64) error {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if !s.canManage(ctx, repo, userID) {
		return fmt.Errorf("forbidden")
	}
	issue, err := s.issues.GetByNumber(ctx, repo.ID, number)
	if err != nil {
		return fmt.Errorf("issue not found: %w", err)
	}
	if issue.IsPinned {
		return nil // idempotent
	}
	count, err := s.issues.CountPinnedByRepo(ctx, repo.ID)
	if err != nil {
		return err
	}
	if msg := pinLimitGuard(count); msg != "" {
		return fmt.Errorf("%s", msg)
	}
	return s.issues.SetPinned(ctx, issue.ID, true)
}

// UnpinIssue unpins an issue. Requires CanManage.
func (s *IssueService) UnpinIssue(ctx context.Context, owner, repoName string, number int, userID int64) error {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if !s.canManage(ctx, repo, userID) {
		return fmt.Errorf("forbidden")
	}
	issue, err := s.issues.GetByNumber(ctx, repo.ID, number)
	if err != nil {
		return fmt.Errorf("issue not found: %w", err)
	}
	return s.issues.SetPinned(ctx, issue.ID, false)
}

// LockIssue locks an issue. Requires CanManage.
func (s *IssueService) LockIssue(ctx context.Context, owner, repoName string, number int, userID int64) error {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if !s.canManage(ctx, repo, userID) {
		return fmt.Errorf("forbidden")
	}
	issue, err := s.issues.GetByNumber(ctx, repo.ID, number)
	if err != nil {
		return fmt.Errorf("issue not found: %w", err)
	}
	return s.issues.SetLocked(ctx, issue.ID, true)
}

// UnlockIssue unlocks an issue. Requires CanManage.
func (s *IssueService) UnlockIssue(ctx context.Context, owner, repoName string, number int, userID int64) error {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if !s.canManage(ctx, repo, userID) {
		return fmt.Errorf("forbidden")
	}
	issue, err := s.issues.GetByNumber(ctx, repo.ID, number)
	if err != nil {
		return fmt.Errorf("issue not found: %w", err)
	}
	return s.issues.SetLocked(ctx, issue.ID, false)
}

// ListPinned returns all pinned issues for a repo.
func (s *IssueService) ListPinned(ctx context.Context, owner, repoName string) ([]model.Issue, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	return s.issues.ListPinned(ctx, repo.ID)
}

// canManage checks whether userID can manage the repo (owner or org owner).
func (s *IssueService) canManage(ctx context.Context, repo *model.Repository, userID int64) bool {
	return s.repoSvc.CanManage(ctx, repo, userID)
}

func (s *IssueService) CountCreatedSince(ctx context.Context, repoID int64, since time.Time) (int, error) {
	return s.issues.CountCreatedSince(ctx, repoID, since)
}

func (s *IssueService) CountClosedSince(ctx context.Context, repoID int64, since time.Time) (int, error) {
	return s.issues.CountClosedSince(ctx, repoID, since)
}
