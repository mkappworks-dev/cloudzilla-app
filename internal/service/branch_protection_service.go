package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
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
	ErrPushRequiresPR        = errors.New("pull request required by branch protection")
	ErrInvalidPattern        = errors.New("invalid branch pattern")
)

// errPullRequestRequired names the rule so the pusher can tell which one
// refused them.
func errPullRequestRequired(rule *model.BranchProtection) error {
	return fmt.Errorf("%w: rule %q", ErrPushRequiresPR, rule.Pattern)
}

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

// Create rejects with ErrInvalidPattern a glob that MatchForBranch would skip, since the rule would never apply.
func (s *BranchProtectionService) Create(ctx context.Context, bp *model.BranchProtection) error {
	if _, err := filepath.Match(bp.Pattern, ""); err != nil {
		return fmt.Errorf("%w: %q is not a valid glob", ErrInvalidPattern, bp.Pattern)
	}
	return s.protections.Create(ctx, bp)
}

func (s *BranchProtectionService) List(ctx context.Context, repoID int64) ([]*model.BranchProtection, error) {
	return s.protections.ListByRepo(ctx, repoID)
}

func (s *BranchProtectionService) Update(ctx context.Context, id, repoID int64, requireReviewCount int, requireStatusChecks model.StringSlice, blockForcePush, requirePullRequest bool) error {
	// Verify the rule belongs to this repo before updating.
	bp, err := s.protections.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if bp.RepoID != repoID {
		return errors.New("branch protection rule not found")
	}
	return s.protections.Update(ctx, id, requireReviewCount, requireStatusChecks, blockForcePush, requirePullRequest)
}

func (s *BranchProtectionService) Delete(ctx context.Context, id, repoID int64) error {
	return s.protections.Delete(ctx, id, repoID)
}

// CheckPush enforces branch protection rules for a push operation.
// isForcePush should be true when the push is non-fast-forward.
func (s *BranchProtectionService) CheckPush(ctx context.Context, repoID int64, branchName string, isForcePush bool) error {
	return s.checkPush(ctx, repoID, branchName, func() bool { return isForcePush })
}

// checkPush is CheckPush, calling isForcePush only if a rule blocks force
// pushes: for a receive-pack command, it walks history.
func (s *BranchProtectionService) checkPush(ctx context.Context, repoID int64, branchName string, isForcePush func() bool) error {
	rule, err := s.protections.MatchForBranch(ctx, repoID, branchName)
	if err != nil || rule == nil {
		return err
	}
	if rule.RequirePullRequest {
		return errPullRequestRequired(rule)
	}
	if rule.BlockForcePush && isForcePush() {
		return ErrForcePushBlocked
	}
	return nil
}

// CheckWebCommit refuses a commit made from the browser to a branch whose
// rule requires a pull request. Such a commit is always a fast-forward, so
// block_force_push has nothing to say about it.
func (s *BranchProtectionService) CheckWebCommit(ctx context.Context, repoID int64, branchName string) error {
	rule, err := s.protections.MatchForBranch(ctx, repoID, branchName)
	if err != nil || rule == nil {
		return err
	}
	if rule.RequirePullRequest {
		return errPullRequestRequired(rule)
	}
	return nil
}

// CheckDelete is CheckPush for deleting a branch outside receive-pack:
// deleting and pushing again is a force push in two steps.
func (s *BranchProtectionService) CheckDelete(ctx context.Context, repoID int64, branchName string) error {
	return s.CheckPush(ctx, repoID, branchName, true)
}

// CheckPushCommand is CheckPush for one receive-pack command. gitRepo must
// already hold the pushed commits. go-git reports the error's text to the
// pusher, so any error but ErrForcePushBlocked and ErrPushRequiresPR is logged
// and returned as ErrProtectionCheckFailed.
func (s *BranchProtectionService) CheckPushCommand(ctx context.Context, repoID int64, gitRepo *gogit.Repository, cmd *packp.Command) error {
	branch, ok := strings.CutPrefix(cmd.Name.String(), "refs/heads/")
	if !ok {
		return nil
	}
	err := s.checkPush(ctx, repoID, branch, func() bool { return !isFastForward(gitRepo, cmd) })
	if err != nil && !errors.Is(err, ErrForcePushBlocked) && !errors.Is(err, ErrPushRequiresPR) {
		slog.Error("BranchProtectionService.CheckPushCommand: rule lookup failed", "repo_id", repoID, "ref", cmd.Name.String(), "error", err)
		return ErrProtectionCheckFailed
	}
	return err
}

// isFastForward reports whether cmd provably keeps every commit its branch
// had. Deleting and pushing again is a force push in two steps, so a delete is
// not a fast-forward; failing closed, neither is anything it can't read as
// commits.
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
	ok, err := isAncestor(gitRepo, oldCommit, newCommit)
	return err == nil && ok
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
