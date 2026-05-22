package service

import (
	"context"
	"fmt"
	"log/slog"

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

// CIChecks holds the required and passing check counts for a PR.
type CIChecks struct{ Required, Passing int }

// CountsByPullIDs omits PRs with an empty HeadSHA from the result map.
func (s *CommitStatusService) CountsByPullIDs(ctx context.Context, pullIDs []int64) (map[int64]CIChecks, error) {
	if s.pulls == nil || s.protections == nil || s.repos == nil {
		return map[int64]CIChecks{}, nil
	}
	prs, err := s.pulls.GetManyByIDs(ctx, pullIDs)
	if err != nil {
		return nil, fmt.Errorf("ci counts: load prs: %w", err)
	}

	repoIDSet := make(map[int64]struct{}, len(prs))
	for _, pr := range prs {
		repoIDSet[pr.RepoID] = struct{}{}
	}
	repoByID := make(map[int64]*model.Repository, len(repoIDSet))
	for repoID := range repoIDSet {
		repo, err := s.repos.GetByID(ctx, repoID)
		if err != nil || repo == nil {
			slog.Warn("ci counts: repo load failed; its PRs will show no CI badge",
				"repo_id", repoID, "error", err)
			continue
		}
		repoByID[repoID] = repo
	}

	// Cache branch-protection rules per (repoID, baseBranch) to avoid repeat lookups.
	type bpKey struct {
		repoID     int64
		baseBranch string
	}
	bpCache := map[bpKey][]string{}

	result := make(map[int64]CIChecks, len(prs))
	for _, pr := range prs {
		if pr.HeadSHA == "" {
			continue
		}
		repo := repoByID[pr.RepoID]
		if repo == nil {
			continue
		}
		k := bpKey{pr.RepoID, pr.BaseBranch}
		requiredContexts, cached := bpCache[k]
		if !cached {
			rule, err := s.protections.MatchForBranch(ctx, pr.RepoID, pr.BaseBranch)
			if err != nil {
				slog.Warn("ci counts: branch protection lookup failed",
					"repo_id", pr.RepoID, "branch", pr.BaseBranch, "error", err)
			} else if rule != nil {
				requiredContexts = []string(rule.RequireStatusChecks)
			}
			bpCache[k] = requiredContexts
		}
		if len(requiredContexts) == 0 {
			continue
		}
		statuses, err := s.statuses.ListBySHA(ctx, pr.RepoID, pr.HeadSHA)
		if err != nil {
			slog.Warn("ci counts: status lookup failed",
				"pull_id", pr.ID, "sha", pr.HeadSHA, "error", err)
			continue
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
		result[pr.ID] = CIChecks{Required: len(requiredContexts), Passing: pass}
	}
	return result, nil
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
	// Prefer the cached head SHA (kept fresh on push); resolve the ref only as a fallback.
	headSHA := pr.HeadSHA
	if headSHA == "" {
		headCommit, _, err := s.code.ResolveRef(repo.OwnerName, repo.Name, pr.HeadBranch)
		if err != nil {
			return len(requiredContexts), 0, fmt.Errorf("resolve head ref %q: %w", pr.HeadBranch, err)
		}
		if headCommit == nil {
			return len(requiredContexts), 0, fmt.Errorf("resolve head ref %q: head commit not found", pr.HeadBranch)
		}
		headSHA = headCommit.Hash.String()
	}
	statuses, err := s.statuses.ListBySHA(ctx, repo.ID, headSHA)
	if err != nil {
		return len(requiredContexts), 0, fmt.Errorf("list statuses for head %s: %w", headSHA, err)
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
