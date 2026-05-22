package service

import (
	"context"
	"fmt"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// pinLimitGuard returns an error string if pinnedCount is at or above the 3-pin limit.
// An empty string means no error.
func pinLimitGuard(pinnedCount int) string {
	if pinnedCount >= 3 {
		return "repositories may not have more than 3 pinned issues"
	}
	return ""
}

// IssueService manages issue lifecycle including creation, state transitions, and visibility.
type IssueService struct {
	issues   *store.IssueStore
	repos    *store.RepoStore
	pulls    *store.PullStore
	repoSvc  *RepoService
	mentions *store.MentionStore
}

// NewIssueService creates an IssueService backed by the given stores.
func NewIssueService(issues *store.IssueStore, repos *store.RepoStore, pulls *store.PullStore, repoSvc *RepoService) *IssueService {
	return &IssueService{issues: issues, repos: repos, pulls: pulls, repoSvc: repoSvc}
}

func (s *IssueService) WithMentionStore(m *store.MentionStore) *IssueService {
	s.mentions = m
	return s
}

func (s *IssueService) Create(ctx context.Context, owner, repoName string, authorID int64, title, body, visibility string) (*model.Issue, error) {
	if len(title) > MaxTitleLen {
		return nil, ErrTitleTooLong
	}
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	if visibility == "" {
		visibility = "public"
	}
	if !s.repoSvc.CanRead(ctx, repo, &authorID) {
		return nil, fmt.Errorf("forbidden: cannot create issues on this repository")
	}
	if visibility == "private" && !s.repoSvc.CanWrite(ctx, repo, authorID) {
		return nil, fmt.Errorf("forbidden: only collaborators with write access may create private issues")
	}
	issue := &model.Issue{
		RepoID:     repo.ID,
		AuthorID:   authorID,
		Title:      title,
		Body:       body,
		State:      model.IssueStateOpen,
		Visibility: visibility,
	}
	if err := s.issues.Create(ctx, issue); err != nil {
		return nil, err
	}
	return issue, nil
}

func (s *IssueService) List(ctx context.Context, owner, repoName string, visibleToUserID *int64) ([]model.Issue, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	return s.issues.ListByRepo(ctx, repo.ID, nil, visibleToUserID, 1, 500)
}

func (s *IssueService) LinkPull(ctx context.Context, pullID, issueID int64) error {
	return s.issues.LinkToPull(ctx, pullID, issueID)
}

func (s *IssueService) UnlinkPull(ctx context.Context, pullID, issueID int64) error {
	return s.issues.UnlinkFromPull(ctx, pullID, issueID)
}

func (s *IssueService) LinkedForPull(ctx context.Context, pullID int64) ([]model.Issue, error) {
	return s.issues.ListLinkedToPull(ctx, pullID)
}

func (s *IssueService) Get(ctx context.Context, owner, repoName string, number int, visibleToUserID *int64) (*model.Issue, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	return s.issues.GetByNumber(ctx, repo.ID, number, visibleToUserID)
}

func (s *IssueService) SetState(ctx context.Context, owner, repoName string, number int, state model.IssueState) (*model.Issue, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	issue, err := s.issues.GetByNumberUnfiltered(ctx, repo.ID, number)
	if err != nil {
		return nil, err
	}
	if err := s.issues.UpdateState(ctx, issue.ID, state); err != nil {
		return nil, err
	}
	// Re-fetch so closed_at and updated_at reflect DB values
	return s.issues.GetByNumberUnfiltered(ctx, repo.ID, number)
}

// SetPriority clears the priority when priority is nil.
func (s *IssueService) SetPriority(ctx context.Context, owner, repoName string, number int, priority *string) (*model.Issue, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	issue, err := s.issues.GetByNumberUnfiltered(ctx, repo.ID, number)
	if err != nil {
		return nil, err
	}
	if err := s.issues.UpdatePriority(ctx, issue.ID, priority); err != nil {
		return nil, err
	}
	return s.issues.GetByNumberUnfiltered(ctx, repo.ID, number)
}

func (s *IssueService) EditTitle(ctx context.Context, owner, repoName string, number int, title string) (*model.Issue, error) {
	if len(title) > MaxTitleLen {
		return nil, ErrTitleTooLong
	}
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	issue, err := s.issues.GetByNumberUnfiltered(ctx, repo.ID, number)
	if err != nil {
		return nil, err
	}
	if err := s.issues.UpdateTitle(ctx, issue.ID, title); err != nil {
		return nil, err
	}
	return s.issues.GetByNumberUnfiltered(ctx, repo.ID, number)
}

func (s *IssueService) EditBody(ctx context.Context, owner, repoName string, number int, body string) (*model.Issue, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	issue, err := s.issues.GetByNumberUnfiltered(ctx, repo.ID, number)
	if err != nil {
		return nil, err
	}
	if err := s.issues.UpdateBody(ctx, issue.ID, body); err != nil {
		return nil, err
	}
	return s.issues.GetByNumberUnfiltered(ctx, repo.ID, number)
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
	issue, err := s.issues.GetByNumberUnfiltered(ctx, repo.ID, number)
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
	issue, err := s.issues.GetByNumberUnfiltered(ctx, repo.ID, number)
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
	issue, err := s.issues.GetByNumberUnfiltered(ctx, repo.ID, number)
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
	issue, err := s.issues.GetByNumberUnfiltered(ctx, repo.ID, number)
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

func (s *IssueService) WeeklyCreated(ctx context.Context, repoID int64, weeks int) ([]int, error) {
	return s.issues.WeeklyCreated(ctx, repoID, weeks)
}

func (s *IssueService) CountOpenAssignedTo(ctx context.Context, userID int64) (int, error) {
	return s.issues.CountOpenAssignedTo(ctx, userID)
}

func (s *IssueService) CountDueThisWeekAssignedTo(ctx context.Context, userID int64) (int, error) {
	return s.issues.CountDueThisWeekAssignedTo(ctx, userID)
}

func (s *IssueService) CountOpen(ctx context.Context, repoID int64) (int, error) {
	return s.issues.CountOpen(ctx, repoID)
}

// mode is "assigned", "created", or "mentioned"; state is "open" or "closed".
func (s *IssueService) ListForUser(ctx context.Context, userID int64, mode, state string) ([]store.IssueListItem, error) {
	if state != "closed" {
		state = "open"
	}
	switch mode {
	case "mentioned":
		ids, err := s.mentions.ListIssueIDsMentioning(ctx, userID)
		if err != nil {
			return nil, err
		}
		return s.issues.ListByIDs(ctx, userID, ids, state)
	case "created":
		return s.issues.ListForUser(ctx, userID, "created", state)
	default:
		return s.issues.ListForUser(ctx, userID, "assigned", state)
	}
}

// CountsForUser returns issue counts keyed "<filter>:<state>" for every
// filter/state combination shown in the account issues tab bar.
func (s *IssueService) CountsForUser(ctx context.Context, userID int64) (map[string]int, error) {
	return s.issues.CountsForUser(ctx, userID)
}

func (s *IssueService) LinkedPRs(ctx context.Context, owner, repoName string, issueNumber int) ([]model.PullRequest, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	return s.pulls.ListLinkedToIssue(ctx, repo.ID, issueNumber)
}
