package service

import (
	"context"
	"log/slog"
	"path"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// DispatchWorkflow is a workflow file that can be started by hand.
type DispatchWorkflow struct{ Path, Name string }

// ListRunsPage returns one page (1-based) of the repo's Runs matching f, newest first, and whether another follows.
func (s *WorkflowRunService) ListRunsPage(ctx context.Context, repoID int64, f model.RunFilter, page, size int) ([]model.WorkflowRun, bool, error) {
	return s.runs.ListRunsFiltered(ctx, repoID, f, size, (max(page, 1)-1)*size)
}

func (s *WorkflowRunService) RunFacets(ctx context.Context, repoID int64) (model.RunFacets, error) {
	return s.runs.RunFacets(ctx, repoID)
}

// RepoCIEnabled reports the repo's own CI switch.
func (s *WorkflowRunService) RepoCIEnabled(ctx context.Context, repoID int64) (bool, error) {
	return s.runs.RepoCIEnabled(ctx, repoID)
}

// ActionsVisible reports whether the Actions tab shows for the repo: the Instance switch is on, and CI is on for the
// repo or it keeps history worth reading.
func (s *WorkflowRunService) ActionsVisible(ctx context.Context, repoID int64) bool {
	if !s.cfg.Enabled {
		return false
	}
	ok, err := s.runs.ActionsVisible(ctx, repoID)
	if err != nil {
		slog.Warn("actions tab visibility failed; showing the tab", "repo_id", repoID, "error", err)
		return true
	}
	return ok
}

// DispatchableWorkflows lists the workflow files at the repo's default branch that have a workflow_dispatch trigger.
func (s *WorkflowRunService) DispatchableWorkflows(ctx context.Context, repo *model.Repository) []DispatchWorkflow {
	commit, _, err := s.code.ResolveRef(repo.OwnerName, repo.Name, repo.DefaultBranch)
	if err != nil {
		return nil
	}
	files, err := s.code.WorkflowFilesAt(repo.OwnerName, repo.Name, commit.Hash.String())
	if err != nil {
		slog.Warn("dispatch: reading workflow files failed", "repo_id", repo.ID, "error", err)
		return nil
	}
	var out []DispatchWorkflow
	for _, f := range files {
		wf, perr := s.parseWorkflow(f.Path, f.Content, f.Err)
		if perr != "" || !wf.On.Dispatch {
			continue
		}
		name := wf.Name
		if name == "" {
			name = strings.TrimSuffix(path.Base(f.Path), path.Ext(f.Path))
		}
		out = append(out, DispatchWorkflow{Path: f.Path, Name: name})
	}
	return out
}

// DispatchRefs lists the branches and tags a workflow can be dispatched on.
func (s *WorkflowRunService) DispatchRefs(repo *model.Repository) (branches, tags []string) {
	refs, err := s.code.ListRefs(repo.OwnerName, repo.Name, repo.DefaultBranch)
	if err != nil {
		return nil, nil
	}
	for _, b := range refs.Branches {
		branches = append(branches, b.Name)
	}
	for _, t := range refs.Tags {
		tags = append(tags, t.Name)
	}
	return branches, tags
}

// RunRefs names the people and pull requests that a set of Runs point at.
type RunRefs struct {
	Users       map[int64]string
	PullNumbers map[int64]int
}

// LoadRunRefs resolves trigger users, cancelling users and pull request numbers in two queries. A failed lookup
// leaves that map empty, so a list still renders with generic wording.
func (s *WorkflowRunService) LoadRunRefs(ctx context.Context, runs []model.WorkflowRun) RunRefs {
	refs := RunRefs{Users: map[int64]string{}, PullNumbers: map[int64]int{}}
	var userIDs, pullIDs []int64
	seenU, seenP := map[int64]bool{}, map[int64]bool{}
	for _, r := range runs {
		for _, id := range []*int64{r.TriggerUserID, r.CancelledBy} {
			if id != nil && !seenU[*id] {
				seenU[*id] = true
				userIDs = append(userIDs, *id)
			}
		}
		if r.PullRequestID != nil && !seenP[*r.PullRequestID] {
			seenP[*r.PullRequestID] = true
			pullIDs = append(pullIDs, *r.PullRequestID)
		}
	}
	if m, err := s.users.UsernamesByIDs(ctx, userIDs); err == nil {
		refs.Users = m
	} else {
		slog.Warn("run refs: usernames failed", "error", err)
	}
	if len(pullIDs) > 0 {
		if prs, err := s.pulls.GetManyByIDs(ctx, pullIDs); err == nil {
			for _, p := range prs {
				refs.PullNumbers[p.ID] = p.Number
			}
		} else {
			slog.Warn("run refs: pull requests failed", "error", err)
		}
	}
	return refs
}
