package service

import (
	"context"
	"fmt"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// PullService manages pull request creation, state transitions, and merge operations.
type PullService struct {
	pulls *store.PullStore
	repos *store.RepoStore
}

// NewPullService creates a PullService backed by the given stores.
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

// autoMergeGuard validates that auto-merge can be enabled. Returns "" if valid,
// or a human-readable error string. Package-private so the test file can call it.
func autoMergeGuard(pr *model.PullRequest, strategy string) string {
	if pr.State != model.PRStateOpen {
		return "auto-merge requires an open pull request"
	}
	if pr.IsDraft {
		return "cannot enable auto-merge on a draft pull request"
	}
	switch strategy {
	case "ff", "merge", "squash":
	default:
		return "strategy must be ff, merge, or squash"
	}
	return ""
}

func (s *PullService) EnableAutoMerge(ctx context.Context, owner, repoName string, number int, userID int64, strategy string) error {
	pr, err := s.Get(ctx, owner, repoName, number)
	if err != nil {
		return err
	}
	if msg := autoMergeGuard(pr, strategy); msg != "" {
		return fmt.Errorf("%s", msg)
	}
	return s.pulls.SetAutoMerge(ctx, pr.ID, true, strategy)
}

func (s *PullService) DisableAutoMerge(ctx context.Context, owner, repoName string, number int, userID int64) error {
	pr, err := s.Get(ctx, owner, repoName, number)
	if err != nil {
		return err
	}
	if pr.State == model.PRStateMerged {
		return fmt.Errorf("cannot change auto-merge on a merged pull request")
	}
	return s.pulls.SetAutoMerge(ctx, pr.ID, false, "")
}

func (s *PullService) ListOpen(ctx context.Context, owner, repoName string) ([]model.PullRequest, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	return s.pulls.ListOpen(ctx, repo.ID)
}

func (s *PullService) GetByID(ctx context.Context, id int64) (*model.PullRequest, error) {
	return s.pulls.GetByID(ctx, id)
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

func (s *PullService) CountCreatedSince(ctx context.Context, repoID int64, since time.Time) (int, error) {
	return s.pulls.CountCreatedSince(ctx, repoID, since)
}

func (s *PullService) CountMergedSince(ctx context.Context, repoID int64, since time.Time) (int, error) {
	return s.pulls.CountMergedSince(ctx, repoID, since)
}

func (s *PullService) CountOpen(ctx context.Context, repoID int64) (int, error) {
	return s.pulls.CountOpen(ctx, repoID)
}
