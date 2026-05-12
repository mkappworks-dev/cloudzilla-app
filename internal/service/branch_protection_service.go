package service

import (
	"context"
	"errors"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

var (
	ErrForcePushBlocked    = errors.New("force push blocked by branch protection")
	ErrInsufficientReviews = errors.New("insufficient reviews for merge")
	ErrStatusCheckFailed   = errors.New("required status checks have not passed")
)

// BranchProtectionService manages branch protection rules and enforces them on push.
type BranchProtectionService struct {
	protections   *store.BranchProtectionStore
	pullReviews   *store.PullReviewStore
	commitStatuses *store.CommitStatusStore
}

// NewBranchProtectionService creates a BranchProtectionService backed by the given stores.
func NewBranchProtectionService(
	protections *store.BranchProtectionStore,
	pullReviews *store.PullReviewStore,
	commitStatuses *store.CommitStatusStore,
) *BranchProtectionService {
	return &BranchProtectionService{
		protections:    protections,
		pullReviews:    pullReviews,
		commitStatuses: commitStatuses,
	}
}

func (s *BranchProtectionService) Create(ctx context.Context, bp *model.BranchProtection) error {
	return s.protections.Create(ctx, bp)
}

func (s *BranchProtectionService) List(ctx context.Context, repoID int64) ([]*model.BranchProtection, error) {
	return s.protections.ListByRepo(ctx, repoID)
}

func (s *BranchProtectionService) Update(ctx context.Context, id, repoID int64, requireReviewCount int, requireStatusChecks model.StringSlice, blockForcePush bool) error {
	// Verify the rule belongs to this repo before updating.
	bp, err := s.protections.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if bp.RepoID != repoID {
		return errors.New("branch protection rule not found")
	}
	return s.protections.Update(ctx, id, requireReviewCount, requireStatusChecks, blockForcePush)
}

func (s *BranchProtectionService) Delete(ctx context.Context, id, repoID int64) error {
	return s.protections.Delete(ctx, id, repoID)
}

// CheckPush enforces branch protection rules for a push operation.
// isForcePush should be true when the push is non-fast-forward.
func (s *BranchProtectionService) CheckPush(ctx context.Context, repoID int64, branchName string, isForcePush bool) error {
	rule, err := s.protections.MatchForBranch(ctx, repoID, branchName)
	if err != nil || rule == nil {
		return err
	}
	if isForcePush && rule.BlockForcePush {
		return ErrForcePushBlocked
	}
	return nil
}

// CheckMerge enforces branch protection rules before a PR merge.
// pr.HeadBranch is used to look up the latest commit status; pr.BaseBranch is the protected target.
// headSHA is the current HEAD commit of the PR's head branch.
func (s *BranchProtectionService) CheckMerge(ctx context.Context, repoID int64, pr *model.PullRequest, headSHA string) error {
	rule, err := s.protections.MatchForBranch(ctx, repoID, pr.BaseBranch)
	if err != nil || rule == nil {
		return err
	}

	if rule.RequireReviewCount > 0 {
		approvals, err := s.pullReviews.CountApprovals(ctx, pr.ID)
		if err != nil {
			return err
		}
		if approvals < rule.RequireReviewCount {
			return ErrInsufficientReviews
		}
	}

	if len(rule.RequireStatusChecks) > 0 && headSHA != "" {
		statuses, err := s.commitStatuses.ListBySHA(ctx, repoID, headSHA)
		if err != nil {
			return err
		}
		statusMap := make(map[string]model.CommitStatusState, len(statuses))
		for _, cs := range statuses {
			statusMap[cs.Context] = cs.State
		}
		for _, required := range rule.RequireStatusChecks {
			state, ok := statusMap[required]
			if !ok || state != model.CommitStatusSuccess {
				return ErrStatusCheckFailed
			}
		}
	}

	return nil
}
