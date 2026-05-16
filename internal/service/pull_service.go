package service

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
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
	commentStore  *store.CommentStore
	contribStats  *store.ContributorStatsStore
	userStore     *store.UserStore
}

// NewPullService creates a PullService backed by the given stores.
func NewPullService(pulls *store.PullStore, repos *store.RepoStore, repoSvc *RepoService) *PullService {
	return &PullService{pulls: pulls, repos: repos, repoSvc: repoSvc}
}

func (s *PullService) WithCIDeps(code *CodeService, commitStatus *CommitStatusService, reviews *store.PullReviewStore, labels *store.LabelStore, assignees *store.AssigneeStore, comments *store.CommentStore) *PullService {
	s.code = code
	s.commitStatus = commitStatus
	s.reviewStore = reviews
	s.labelStore = labels
	s.assigneeStore = assignees
	s.commentStore = comments
	return s
}

type PullListRow struct {
	model.PullRequest
	HeadSHA       string
	CIStatus      string
	CIPassing     int
	CITotal       int
	CommentCount  int
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

	pullIDs := make([]int64, len(pulls))
	for i, p := range pulls {
		pullIDs[i] = p.ID
	}

	// Batch the per-PR sidebar lookups so the list page issues a constant
	// number of queries rather than one set per row.
	var reviewsByPull map[int64][]model.PullReview
	if s.reviewStore != nil {
		if reviewsByPull, err = s.reviewStore.ListByPullIDs(ctx, pullIDs); err != nil {
			return nil, fmt.Errorf("list reviews: %w", err)
		}
	}
	var labelsByPull map[int64][]model.Label
	if s.labelStore != nil {
		if labelsByPull, err = s.labelStore.ListByPullIDs(ctx, pullIDs); err != nil {
			return nil, fmt.Errorf("list labels: %w", err)
		}
	}
	var assigneesByPull map[int64][]model.User
	if s.assigneeStore != nil {
		if assigneesByPull, err = s.assigneeStore.ListByPullIDs(ctx, pullIDs); err != nil {
			return nil, fmt.Errorf("list assignees: %w", err)
		}
	}
	var commentsByPull map[int64]int
	if s.commentStore != nil {
		if commentsByPull, err = s.commentStore.CountByPullIDs(ctx, pullIDs); err != nil {
			return nil, fmt.Errorf("count comments: %w", err)
		}
	}

	out := make([]PullListRow, 0, len(pulls))
	for _, p := range pulls {
		row := PullListRow{
			PullRequest:   p,
			Reviewers:     reviewsByPull[p.ID],
			LabelChips:    labelsByPull[p.ID],
			AssigneeChips: assigneesByPull[p.ID],
			CommentCount:  commentsByPull[p.ID],
		}
		if s.code != nil {
			if commit, _, err := s.code.ResolveRef(owner, repoName, p.HeadBranch); err == nil {
				row.HeadSHA = commit.Hash.String()
				if s.commitStatus != nil {
					if combined, statuses, err := s.commitStatus.GetCombined(ctx, owner, repoName, row.HeadSHA); err == nil {
						row.CIStatus = string(combined)
						row.CITotal = len(statuses)
						for _, st := range statuses {
							if st.State == model.CommitStatusSuccess {
								row.CIPassing++
							}
						}
					}
				}
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

// UpdateTitle renames an open or closed pull request. title must be pre-trimmed.
func (s *PullService) UpdateTitle(ctx context.Context, owner, repoName string, number int, title string) (*model.PullRequest, error) {
	pr, err := s.Get(ctx, owner, repoName, number)
	if err != nil {
		return nil, err
	}
	if pr.State == model.PRStateMerged {
		return nil, fmt.Errorf("merged PRs cannot be updated")
	}
	if title == "" {
		return nil, fmt.Errorf("title cannot be empty")
	}
	if err := s.pulls.UpdateTitle(ctx, pr.ID, title); err != nil {
		return nil, err
	}
	return s.Get(ctx, owner, repoName, number)
}

// UpdateBody edits the description of an open or closed pull request.
func (s *PullService) UpdateBody(ctx context.Context, owner, repoName string, number int, body string) (*model.PullRequest, error) {
	pr, err := s.Get(ctx, owner, repoName, number)
	if err != nil {
		return nil, err
	}
	if pr.State == model.PRStateMerged {
		return nil, fmt.Errorf("merged PRs cannot be updated")
	}
	if err := s.pulls.UpdateBody(ctx, pr.ID, body); err != nil {
		return nil, err
	}
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

func (s *PullService) WithReviewerDeps(contribStats *store.ContributorStatsStore, userStore *store.UserStore) *PullService {
	s.contribStats = contribStats
	s.userStore = userStore
	return s
}

// SuggestReviewers returns up to limit candidate reviewers for a PR between base and head.
// Preference order: (1) CODEOWNERS matches on the repo's default branch; (2) top contributors by commit count.
func (s *PullService) SuggestReviewers(ctx context.Context, owner, repoName, base, head string, limit int) ([]model.User, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, err
	}

	if s.code != nil {
		// Missing CODEOWNERS is normal; error is intentionally ignored and the contributor fallback is used.
		rules, _ := s.code.GetCodeOwners(owner, repoName, repo.DefaultBranch)
		if len(rules) > 0 {
			diff, diffErr := s.code.GetPullDiff(owner, repoName, base, head)
			if diffErr != nil {
				slog.Warn("suggest reviewers: pull diff lookup failed", "owner", owner, "repo", repoName, "error", diffErr)
			}
			if diff != nil {
				changed := make([]string, 0, len(diff.Files))
				for _, f := range diff.Files {
					if f.NewPath != "" {
						changed = append(changed, f.NewPath)
					} else {
						changed = append(changed, f.OldPath)
					}
				}
				owners := s.code.MatchCodeOwners(rules, changed)
				if len(owners) > 0 && s.userStore != nil {
					users, usersErr := s.userStore.GetManyByUsernames(ctx, owners)
					if usersErr != nil {
						slog.Warn("suggest reviewers: resolve CODEOWNERS usernames failed", "owner", owner, "repo", repoName, "error", usersErr)
					}
					if usersErr == nil && len(users) > 0 {
						if limit > 0 && len(users) > limit {
							users = users[:limit]
						}
						return users, nil
					}
				}
			}
		}
	}

	if s.contribStats == nil || s.userStore == nil {
		return nil, nil
	}
	rows, err := s.contribStats.ListForRepo(ctx, repo.ID)
	if err != nil {
		return nil, fmt.Errorf("contributor stats lookup: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	counts := map[int64]int{}
	for _, r := range rows {
		counts[r.UserID] += r.Commits
	}
	type pair struct {
		id int64
		n  int
	}
	pairs := make([]pair, 0, len(counts))
	for id, n := range counts {
		pairs = append(pairs, pair{id, n})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].n > pairs[j].n })
	if limit > 0 && len(pairs) > limit {
		pairs = pairs[:limit]
	}
	ids := make([]int64, len(pairs))
	for i, p := range pairs {
		ids[i] = p.id
	}
	return s.userStore.GetManyByIDs(ctx, ids)
}
