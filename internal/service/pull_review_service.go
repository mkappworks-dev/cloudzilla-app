package service

import (
	"context"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// PullReviewService manages PR review submission and approval state.
type PullReviewService struct {
	reviews *store.PullReviewStore
	pulls   *store.PullStore
	repos   *store.RepoStore
}

// NewPullReviewService creates a PullReviewService backed by the given stores.
func NewPullReviewService(reviews *store.PullReviewStore, pulls *store.PullStore, repos *store.RepoStore) *PullReviewService {
	return &PullReviewService{reviews: reviews, pulls: pulls, repos: repos}
}

func (s *PullReviewService) SubmitReview(ctx context.Context, owner, repoName string, pullNumber int, reviewerID int64, reviewerName, state, body string) (*model.PullReview, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	pr, err := s.pulls.GetByNumber(ctx, repo.ID, pullNumber)
	if err != nil {
		return nil, fmt.Errorf("pull request not found: %w", err)
	}
	if reviewerID == pr.AuthorID {
		return nil, fmt.Errorf("cannot review your own pull request")
	}
	r := &model.PullReview{
		PullID:     pr.ID,
		RepoID:     repo.ID,
		AuthorID:   reviewerID,
		AuthorName: reviewerName,
		State:      model.PRReviewState(state),
		Body:       body,
	}
	if err := s.reviews.Upsert(ctx, r); err != nil {
		return nil, fmt.Errorf("upsert review: %w", err)
	}
	return r, nil
}

func (s *PullReviewService) CanMerge(ctx context.Context, pullID int64) (bool, string, error) {
	blocked, err := s.reviews.HasChangesRequested(ctx, pullID)
	if err != nil {
		return true, "", err
	}
	if blocked {
		return false, "changes requested by reviewer", nil
	}
	return true, "", nil
}

func (s *PullReviewService) ListByPull(ctx context.Context, owner, repoName string, pullNumber int) ([]model.PullReview, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	pr, err := s.pulls.GetByNumber(ctx, repo.ID, pullNumber)
	if err != nil {
		return nil, fmt.Errorf("pull request not found: %w", err)
	}
	return s.reviews.ListByPull(ctx, pr.ID)
}
