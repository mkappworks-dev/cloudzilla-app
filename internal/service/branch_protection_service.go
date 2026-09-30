package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

var (
	ErrForcePushBlocked      = errors.New("force push blocked by branch protection")
	ErrProtectionCheckFailed = errors.New("internal error checking branch protection")
	ErrInsufficientReviews   = errors.New("insufficient reviews for merge")
	ErrStatusCheckFailed     = errors.New("required status checks have not passed")
)

// BranchProtectionService manages branch protection rules and enforces them on push.
type BranchProtectionService struct {
	protections    *store.BranchProtectionStore
	pullReviews    *store.PullReviewStore
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

// CheckPushCommand is CheckPush for one receive-pack command. gitRepo must
// already hold the pushed commits. go-git reports the error's text to the
// pusher, so any error but ErrForcePushBlocked is logged and returned as
// ErrProtectionCheckFailed.
func (s *BranchProtectionService) CheckPushCommand(ctx context.Context, repoID int64, gitRepo *gogit.Repository, cmd *packp.Command) error {
	branch, ok := strings.CutPrefix(cmd.Name.String(), "refs/heads/")
	if !ok {
		return nil
	}
	err := s.CheckPush(ctx, repoID, branch, !isFastForward(gitRepo, cmd))
	if err != nil && !errors.Is(err, ErrForcePushBlocked) {
		slog.Error("BranchProtectionService.CheckPushCommand: rule lookup failed", "repo_id", repoID, "ref", cmd.Name.String(), "error", err)
		return ErrProtectionCheckFailed
	}
	return err
}

// isFastForward reports whether cmd provably keeps every commit its branch
// had. Deleting and pushing again is a force push in two steps, and a branch
// can be pushed to an object that isn't a commit, so a delete or anything it
// can't read as commits is not a fast-forward.
func isFastForward(gitRepo *gogit.Repository, cmd *packp.Command) bool {
	if cmd.Action() == packp.Delete {
		return false
	}
	newCommit, err := gitRepo.CommitObject(cmd.New)
	if err != nil {
		return false
	}
	if cmd.Action() == packp.Create {
		return true
	}
	oldCommit, err := gitRepo.CommitObject(cmd.Old)
	if err != nil {
		return false
	}
	isAncestor, err := oldCommit.IsAncestor(newCommit)
	return err == nil && isAncestor
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
