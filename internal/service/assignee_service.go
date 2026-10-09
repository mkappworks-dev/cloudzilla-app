package service

import (
	"context"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// AssigneeService manages assigning and removing users from issues and pull requests.
type AssigneeService struct {
	assignees *store.AssigneeStore
	repos     *store.RepoStore
	issues    *store.IssueStore
	pulls     *store.PullStore
	users     *store.UserStore
	threads   threadAutoSubscriber
}

// NewAssigneeService creates an AssigneeService backed by the given stores.
func NewAssigneeService(assignees *store.AssigneeStore, repos *store.RepoStore, issues *store.IssueStore, pulls *store.PullStore, users *store.UserStore) *AssigneeService {
	return &AssigneeService{assignees: assignees, repos: repos, issues: issues, pulls: pulls, users: users}
}

// WithThreadSubscriptions makes participation subscribe the user to the thread.
func (s *AssigneeService) WithThreadSubscriptions(t threadAutoSubscriber) *AssigneeService {
	s.threads = t
	return s
}

func (s *AssigneeService) AddToIssue(ctx context.Context, owner, repoName string, issueNumber int, username string) error {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	issue, err := s.issues.GetByNumberUnfiltered(ctx, repo.ID, issueNumber)
	if err != nil {
		return fmt.Errorf("issue not found: %w", err)
	}
	user, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("user not found: %w", err)
	}
	if err := s.assignees.AddToIssue(ctx, issue.ID, user.ID); err != nil {
		return err
	}
	autoSubscribe(ctx, s.threads, user.ID, repo.ID, model.ThreadKindIssue, issue.Number, model.ThreadReasonAssign)
	return nil
}

func (s *AssigneeService) RemoveFromIssue(ctx context.Context, owner, repoName string, issueNumber int, username string) error {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	issue, err := s.issues.GetByNumberUnfiltered(ctx, repo.ID, issueNumber)
	if err != nil {
		return fmt.Errorf("issue not found: %w", err)
	}
	user, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("user not found: %w", err)
	}
	return s.assignees.RemoveFromIssue(ctx, issue.ID, user.ID)
}

func (s *AssigneeService) GetForIssue(ctx context.Context, issueID int64) ([]model.User, error) {
	return s.assignees.ListByIssue(ctx, issueID)
}

func (s *AssigneeService) AddToPull(ctx context.Context, owner, repoName string, pullNumber int, username string) error {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	pull, err := s.pulls.GetByNumber(ctx, repo.ID, pullNumber)
	if err != nil {
		return fmt.Errorf("pull not found: %w", err)
	}
	user, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("user not found: %w", err)
	}
	if err := s.assignees.AddToPull(ctx, pull.ID, user.ID); err != nil {
		return err
	}
	autoSubscribe(ctx, s.threads, user.ID, repo.ID, model.ThreadKindPull, pull.Number, model.ThreadReasonAssign)
	return nil
}

func (s *AssigneeService) RemoveFromPull(ctx context.Context, owner, repoName string, pullNumber int, username string) error {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	pull, err := s.pulls.GetByNumber(ctx, repo.ID, pullNumber)
	if err != nil {
		return fmt.Errorf("pull not found: %w", err)
	}
	user, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("user not found: %w", err)
	}
	return s.assignees.RemoveFromPull(ctx, pull.ID, user.ID)
}

func (s *AssigneeService) GetForPull(ctx context.Context, pullID int64) ([]model.User, error) {
	return s.assignees.ListByPull(ctx, pullID)
}
