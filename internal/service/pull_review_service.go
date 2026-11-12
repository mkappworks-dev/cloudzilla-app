package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

var ErrReviewNotFound = errors.New("review not found")

var ErrReviewBodyRequired = errors.New("a summary is required to comment or request changes")

var ErrOwnPullVerdict = errors.New("you can comment on your own pull request, but not approve it or request changes")

var ErrDismissReasonRequired = errors.New("a reason is required to dismiss a review")

var ErrReviewNotDismissible = errors.New("only a standing request for changes can be dismissed")

// MaxDismissReason keeps a pasted essay out of the timeline and the audit log.
const MaxDismissReason = 500

// PullReviewService manages PR review submission and approval state.
type PullReviewService struct {
	reviews     *store.PullReviewStore
	pulls       *store.PullStore
	repos       *store.RepoStore
	protections *store.BranchProtectionStore
	threads     threadAutoSubscriber
}

// `protections` may be nil in test fixtures; Counts will then return (0, 0, nil).
func NewPullReviewService(reviews *store.PullReviewStore, pulls *store.PullStore, repos *store.RepoStore, protections *store.BranchProtectionStore) *PullReviewService {
	return &PullReviewService{reviews: reviews, pulls: pulls, repos: repos, protections: protections}
}

// WithThreadSubscriptions makes participation subscribe the user to the thread.
func (s *PullReviewService) WithThreadSubscriptions(t threadAutoSubscriber) *PullReviewService {
	s.threads = t
	return s
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
	switch model.PRReviewState(state) {
	case model.PRReviewApproved, model.PRReviewChangesRequested:
		if reviewerID == pr.AuthorID {
			return nil, ErrOwnPullVerdict
		}
	case model.PRReviewCommented:
	default:
		return nil, fmt.Errorf("review state must be approved, changes_requested or commented")
	}
	if state != string(model.PRReviewApproved) && strings.TrimSpace(body) == "" {
		return nil, ErrReviewBodyRequired
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
	autoSubscribe(ctx, s.threads, reviewerID, repo.ID, model.ThreadKindPull, pr.Number, model.ThreadReasonReview)
	return r, nil
}

// Dismiss clears the request for changes that blocks pr's merge. The caller checks that byName may manage the repo.
func (s *PullReviewService) Dismiss(ctx context.Context, owner, repoName string, pullNumber int, reviewID int64, byName, reason string) (*model.PullReview, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, ErrDismissReasonRequired
	}
	if len([]rune(reason)) > MaxDismissReason {
		return nil, fmt.Errorf("the reason can be at most %d characters", MaxDismissReason)
	}
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	pr, err := s.pulls.GetByNumber(ctx, repo.ID, pullNumber)
	if err != nil {
		return nil, fmt.Errorf("pull request not found: %w", err)
	}
	if pr.State != model.PRStateOpen {
		return nil, errors.New("only an open pull request has reviews to dismiss")
	}
	if _, err := s.reviews.GetInPull(ctx, pr.ID, reviewID); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrReviewNotFound
	} else if err != nil {
		return nil, fmt.Errorf("load review: %w", err)
	}
	done, err := s.reviews.Dismiss(ctx, pr.ID, reviewID, byName, reason)
	if err != nil {
		return nil, err
	}
	if !done {
		return nil, ErrReviewNotDismissible
	}
	return s.reviews.GetInPull(ctx, pr.ID, reviewID)
}

// ChangesRequestedBy lists the standing requests for changes on a pull request, oldest first.
func (s *PullReviewService) ChangesRequestedBy(ctx context.Context, pullID int64) ([]model.PullReview, error) {
	all, err := s.reviews.ListByPull(ctx, pullID)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(all, func(r model.PullReview) bool { return r.State != model.PRReviewChangesRequested }), nil
}

func (s *PullReviewService) ListReviewersByPullIDs(ctx context.Context, pullIDs []int64) (map[int64][]model.PullReview, error) {
	return s.reviews.ListByPullIDs(ctx, pullIDs)
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
	if len(reviewers) == 0 {
		return nil
	}
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

// A review that has already been submitted is left untouched.
func (s *PullReviewService) WithdrawReviewer(ctx context.Context, owner, repoName string, pullNumber int, reviewer model.User) error {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	pr, err := s.pulls.GetByNumber(ctx, repo.ID, pullNumber)
	if err != nil {
		return fmt.Errorf("pull request not found: %w", err)
	}
	return s.reviews.RemovePendingReview(ctx, pr.ID, reviewer.ID)
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
	// The count CheckMerge enforces, so the page never shows a merge it would refuse.
	approved, err = s.reviews.CountApprovals(ctx, pullID)
	if err != nil {
		return required, 0, fmt.Errorf("count approvals for pull %d: %w", pullID, err)
	}
	return required, approved, nil
}
