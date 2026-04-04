package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

// ErrIssueLocked is returned when a non-maintainer tries to comment on a locked issue.
var ErrIssueLocked = errors.New("issue is locked")

// pinLimitGuard returns an error string if pinnedCount exceeds the 3-pin limit.
// An empty string means no error.
func pinLimitGuard(pinnedCount int) string {
	if pinnedCount > 3 {
		return "repositories may not have more than 3 pinned issues"
	}
	return ""
}

// lockGuard returns an error string if the issue is locked (blocking a comment).
func lockGuard(isLocked bool) string {
	if isLocked {
		return "issue is locked"
	}
	return ""
}

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

// PinIssue pins an issue. Requires CanManage. Maximum 3 pinned issues per repo.
func (s *IssueService) PinIssue(ctx context.Context, owner, repoName string, number int, userID int64) error {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if !s.canManage(repo, userID) {
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
	if !s.canManage(repo, userID) {
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
	if !s.canManage(repo, userID) {
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
	if !s.canManage(repo, userID) {
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

// canManage checks whether userID is the direct repo owner.
func (s *IssueService) canManage(repo *model.Repository, userID int64) bool {
	return repo.OwnerID == userID
}
