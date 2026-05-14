package service

import (
	"context"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// CommitStatusService manages commit status checks from CI/CD integrations.
type CommitStatusService struct {
	statuses    *store.CommitStatusStore
	repos       *store.RepoStore
	pulls       *store.PullStore
	protections *store.BranchProtectionStore
	code        *CodeService
}

// `pulls`, `protections`, and `code` may be nil in test fixtures; Counts will then return (0, 0, nil).
func NewCommitStatusService(statuses *store.CommitStatusStore, repos *store.RepoStore, pulls *store.PullStore, protections *store.BranchProtectionStore, code *CodeService) *CommitStatusService {
	return &CommitStatusService{statuses: statuses, repos: repos, pulls: pulls, protections: protections, code: code}
}

func (s *CommitStatusService) Upsert(ctx context.Context, owner, repoName, sha string, cs *model.CommitStatus) error {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	cs.RepoID = repo.ID
	cs.SHA = sha
	if cs.Context == "" {
		cs.Context = "default"
	}
	return s.statuses.Upsert(ctx, cs)
}

func (s *CommitStatusService) List(ctx context.Context, owner, repoName, sha string) ([]model.CommitStatus, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	return s.statuses.ListBySHA(ctx, repo.ID, sha)
}

func (s *CommitStatusService) GetCombined(ctx context.Context, owner, repoName, sha string) (model.CommitStatusState, []model.CommitStatus, error) {
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return "", nil, fmt.Errorf("repo not found: %w", err)
	}
	statuses, err := s.statuses.ListBySHA(ctx, repo.ID, sha)
	if err != nil {
		return "", nil, err
	}
	combined, err := s.statuses.GetCombined(ctx, repo.ID, sha)
	if err != nil {
		return "", nil, err
	}
	return combined, statuses, nil
}

// Returns (0, 0, nil) when no protection rule matches or required deps are unavailable.
func (s *CommitStatusService) Counts(ctx context.Context, pullID int64) (required int, passing int, err error) {
	if s.pulls == nil || s.protections == nil || s.code == nil {
		return 0, 0, nil
	}
	pr, err := s.pulls.GetByID(ctx, pullID)
	if err != nil || pr == nil {
		return 0, 0, err
	}
	repo, err := s.repos.GetByID(ctx, pr.RepoID)
	if err != nil || repo == nil {
		return 0, 0, err
	}
	rule, err := s.protections.MatchForBranch(ctx, pr.RepoID, pr.BaseBranch)
	if err != nil {
		return 0, 0, err
	}
	if rule == nil {
		return 0, 0, nil
	}
	requiredContexts := []string(rule.RequireStatusChecks)
	if len(requiredContexts) == 0 {
		return 0, 0, nil
	}
	headCommit, _, err := s.code.ResolveRef(repo.OwnerName, repo.Name, pr.HeadBranch)
	if err != nil {
		return len(requiredContexts), 0, fmt.Errorf("resolve head ref %q: %w", pr.HeadBranch, err)
	}
	if headCommit == nil {
		return len(requiredContexts), 0, fmt.Errorf("resolve head ref %q: head commit not found", pr.HeadBranch)
	}
	statuses, err := s.statuses.ListBySHA(ctx, repo.ID, headCommit.Hash.String())
	if err != nil {
		return len(requiredContexts), 0, fmt.Errorf("list statuses for head %s: %w", headCommit.Hash.String(), err)
	}
	passingByCtx := make(map[string]bool, len(statuses))
	for _, st := range statuses {
		if st.State == model.CommitStatusSuccess {
			passingByCtx[st.Context] = true
		}
	}
	pass := 0
	for _, ctxName := range requiredContexts {
		if passingByCtx[ctxName] {
			pass++
		}
	}
	return len(requiredContexts), pass, nil
}
