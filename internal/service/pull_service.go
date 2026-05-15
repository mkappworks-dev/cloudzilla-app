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
	pulls         *store.PullStore
	repos         *store.RepoStore
	repoSvc       *RepoService
	code          *CodeService
	commitStatus  *CommitStatusService
	reviewStore   *store.PullReviewStore
	labelStore    *store.LabelStore
	assigneeStore *store.AssigneeStore
}

// NewPullService creates a PullService backed by the given stores.
func NewPullService(pulls *store.PullStore, repos *store.RepoStore, repoSvc *RepoService) *PullService {
	return &PullService{pulls: pulls, repos: repos, repoSvc: repoSvc}
}

func (s *PullService) WithCIDeps(code *CodeService, commitStatus *CommitStatusService, reviews *store.PullReviewStore, labels *store.LabelStore, assignees *store.AssigneeStore) *PullService {
	s.code = code
	s.commitStatus = commitStatus
	s.reviewStore = reviews
	s.labelStore = labels
	s.assigneeStore = assignees
	return s
}

type PullListRow struct {
	model.PullRequest
	HeadSHA       string
	CIStatus      string
	Reviewers     []model.PullReview
	LabelChips    []model.Label
	AssigneeChips []model.User
}

func (s *PullService) ListWithCIStatus(ctx context.Context, owner, repoName string, state model.PRState, offset, limit int) ([]PullListRow, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	pulls, err := s.pulls.ListByState(ctx, repo.ID, state, offset, limit)
	if err != nil {
		return nil, err
	}
	out := make([]PullListRow, 0, len(pulls))
	for _, p := range pulls {
		row := PullListRow{PullRequest: p}
		if s.code != nil {
			if commit, _, err := s.code.ResolveRef(owner, repoName, p.HeadBranch); err == nil {
				row.HeadSHA = commit.Hash.String()
				if s.commitStatus != nil {
					if combined, _, err := s.commitStatus.GetCombined(ctx, owner, repoName, row.HeadSHA); err == nil {
						row.CIStatus = string(combined)
					}
				}
			}
		}
		if s.reviewStore != nil {
			if rev, err := s.reviewStore.ListByPull(ctx, p.ID); err == nil {
				row.Reviewers = rev
			}
		}
		if s.labelStore != nil {
			if labs, err := s.labelStore.ListByPull(ctx, p.ID); err == nil {
				row.LabelChips = labs
			}
		}
		if s.assigneeStore != nil {
			if asg, err := s.assigneeStore.ListByPull(ctx, p.ID); err == nil {
				row.AssigneeChips = asg
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// ErrPullForbidden is returned when an author lacks read access to the target repo.
var ErrPullForbidden = fmt.Errorf("forbidden: cannot open pull requests on this repository")

func (s *PullService) Create(ctx context.Context, owner, repoName string, authorID int64, title, body, head, base string, isDraft bool) (*model.PullRequest, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	if !s.repoSvc.CanRead(ctx, repo, &authorID) {
		return nil, ErrPullForbidden
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

func (s *PullService) WeeklyCreated(ctx context.Context, repoID int64, weeks int) ([]int, error) {
	return s.pulls.WeeklyCreated(ctx, repoID, weeks)
}

func (s *PullService) CountOpen(ctx context.Context, repoID int64) (int, error) {
	return s.pulls.CountOpen(ctx, repoID)
}

func (s *PullService) CountOpenAuthoredByOrAssignedTo(ctx context.Context, userID int64) (int, error) {
	return s.pulls.CountOpenAuthoredByOrAssignedTo(ctx, userID)
}
