package service

import (
	"context"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// PullReviewService manages PR review submission and approval state.
type PullReviewService struct {
	reviews     *store.PullReviewStore
	pulls       *store.PullStore
	repos       *store.RepoStore
	protections *store.BranchProtectionStore
}

// `protections` may be nil in test fixtures; Counts will then return (0, 0, nil).
func NewPullReviewService(reviews *store.PullReviewStore, pulls *store.PullStore, repos *store.RepoStore, protections *store.BranchProtectionStore) *PullReviewService {
	return &PullReviewService{reviews: reviews, pulls: pulls, repos: repos, protections: protections}
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

func (s *PullReviewService) RequestReviewers(ctx context.Context, owner, repoName string, pullNumber int, reviewers []model.User) error {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	pr, err := s.pulls.GetByNumber(ctx, repo.ID, pullNumber)
	if err != nil {
		return fmt.Errorf("pull request not found: %w", err)
	}
	for _, u := range reviewers {
		if err := s.reviews.RequestReview(ctx, pr.ID, repo.ID, u.ID, u.Username); err != nil {
			return fmt.Errorf("request review for user %d: %w", u.ID, err)
		}
	}
	return nil
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

// Returns (0, 0, nil) when no protection rule matches or required deps are unavailable.
func (s *PullReviewService) Counts(ctx context.Context, pullID int64) (required int, approved int, err error) {
	if s.pulls == nil {
		return 0, 0, nil
	}
	pr, err := s.pulls.GetByID(ctx, pullID)
	if err != nil || pr == nil {
		return 0, 0, err
	}
	if s.protections != nil {
		rule, perr := s.protections.MatchForBranch(ctx, pr.RepoID, pr.BaseBranch)
		if perr != nil {
			return 0, 0, perr
		}
		if rule != nil {
			required = rule.RequireReviewCount
		}
	}
	if required == 0 {
		return 0, 0, nil
	}
	reviews, err := s.reviews.ListByPull(ctx, pullID)
	if err != nil {
		return required, 0, fmt.Errorf("list reviews for pull %d: %w", pullID, err)
	}
	approvers := make(map[int64]bool)
	for _, rev := range reviews {
		if rev.State == model.PRReviewApproved {
			approvers[rev.AuthorID] = true
		}
	}
	return required, len(approvers), nil
}
