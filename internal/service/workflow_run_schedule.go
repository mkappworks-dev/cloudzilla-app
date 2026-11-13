package service

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/ci/workflow"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// advance releases every Job whose needs have finished (queued, or skipped when its if is false) and sets the Run's
// status from its Jobs. A waiting Run holds all its Jobs until it is approved.
func (s *WorkflowRunService) advance(ctx context.Context, tx *store.WorkflowRunTx, run *model.WorkflowRun, repo *model.Repository) error {
	if run.Status == model.CIStatusWaiting {
		return nil
	}
	jobs, err := tx.CurrentJobs(ctx, run.ID)
	if err != nil {
		return err
	}
	for moved := true; moved; {
		moved = false
		for i := range jobs {
			if jobs[i].Status != model.CIStatusWaiting {
				continue
			}
			status, msg, ready := s.release(ctx, run, repo, &jobs[i], jobs)
			if !ready {
				continue
			}
			if err := s.setJobStatus(ctx, tx, run, repo, &jobs[i], status, msg); err != nil {
				return err
			}
			moved = true
		}
	}
	return s.syncRunStatus(ctx, tx, run, jobs)
}

// release decides what a waiting Job becomes; ready is false while a Job it needs is unfinished.
func (s *WorkflowRunService) release(ctx context.Context, run *model.WorkflowRun, repo *model.Repository, job *model.WorkflowJob, all []model.WorkflowJob) (status model.CIStatus, msg string, ready bool) {
	needs := map[string]string{}
	overall := "success"
	for _, key := range job.Needs {
		res, done := needResult(key, all)
		if !done {
			return "", "", false
		}
		needs[key] = res
		overall = worseNeed(overall, res)
	}
	var cond *workflow.Expr
	if job.IfExpr != "" {
		var err error
		if cond, err = workflow.ParseExpr(job.IfExpr); err != nil {
			return model.CIStatusFailure, "invalid if: " + err.Error(), true
		}
	}
	ok, err := cond.Bool(workflow.Context{GitHub: s.githubContext(ctx, run, repo), Env: job.Env, Matrix: job.Matrix, Needs: needs, Status: overall})
	switch {
	case err != nil:
		return model.CIStatusFailure, "if: " + err.Error(), true
	case ok:
		return model.CIStatusQueued, "", true
	}
	return model.CIStatusSkipped, "", true
}

// needResult folds the Jobs sharing a key, a matrix being several, into one result.
func needResult(key string, all []model.WorkflowJob) (result string, done bool) {
	result = string(model.CIStatusSkipped)
	for _, j := range all {
		if j.JobKey != key {
			continue
		}
		if !j.Status.Finished() {
			return "", false
		}
		switch {
		case j.Status == model.CIStatusFailure:
			result = string(model.CIStatusFailure)
		case j.Status == model.CIStatusCancelled && result != string(model.CIStatusFailure):
			result = string(model.CIStatusCancelled)
		case j.Status == model.CIStatusSuccess && result == string(model.CIStatusSkipped):
			result = string(model.CIStatusSuccess)
		}
	}
	return result, true
}

// worseNeed ranks needs results failure > cancelled > skipped > success.
func worseNeed(a, b string) string {
	rank := map[string]int{"success": 0, "skipped": 1, "cancelled": 2, "failure": 3}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func (s *WorkflowRunService) githubContext(ctx context.Context, run *model.WorkflowRun, repo *model.Repository) map[string]string {
	actor := repo.OwnerName
	if run.TriggerUserID != nil {
		if u, err := s.users.GetByID(ctx, *run.TriggerUserID); err == nil {
			actor = u.Username
		}
	}
	refName := strings.TrimPrefix(strings.TrimPrefix(run.Ref, "refs/heads/"), "refs/tags/")
	return map[string]string{
		"sha": run.SHA, "ref": run.Ref, "ref_name": refName, "event_name": run.Event, "actor": actor,
		"repository": repo.OwnerName + "/" + repo.Name, "run_number": strconv.Itoa(run.Number),
	}
}

// syncRunStatus sets the Run's status from its Jobs: running once any has started, final once all have finished.
func (s *WorkflowRunService) syncRunStatus(ctx context.Context, tx *store.WorkflowRunTx, run *model.WorkflowRun, jobs []model.WorkflowJob) error {
	next := runStatusOf(jobs)
	if next == "" || next == run.Status {
		return nil
	}
	run.Status = next
	return tx.UpdateRunState(ctx, run.ID, next)
}

func runStatusOf(jobs []model.WorkflowJob) model.CIStatus {
	if len(jobs) == 0 {
		return ""
	}
	var failed, cancelled, started, open bool
	for _, j := range jobs {
		switch j.Status {
		case model.CIStatusFailure:
			failed, started = true, true
		case model.CIStatusCancelled:
			cancelled = true
		case model.CIStatusSuccess:
			started = true
		case model.CIStatusRunning:
			return model.CIStatusRunning
		case model.CIStatusWaiting, model.CIStatusQueued:
			open = true
		}
	}
	switch {
	case open && started:
		return model.CIStatusRunning
	case open:
		return model.CIStatusQueued
	case failed:
		return model.CIStatusFailure
	case cancelled:
		return model.CIStatusCancelled
	}
	return model.CIStatusSuccess
}

// setJobStatus moves a Job and posts the matching commit status.
func (s *WorkflowRunService) setJobStatus(ctx context.Context, tx *store.WorkflowRunTx, run *model.WorkflowRun, repo *model.Repository, job *model.WorkflowJob, status model.CIStatus, msg string) error {
	if err := tx.SetJobStatus(ctx, job.ID, status, msg); err != nil {
		return err
	}
	job.Status, job.Message = status, msg
	if status.Finished() {
		stepStatus := model.CIStatusSkipped
		if status == model.CIStatusCancelled {
			stepStatus = model.CIStatusCancelled
		}
		if err := tx.FinishOpenSteps(ctx, job.ID, stepStatus); err != nil {
			return err
		}
	}
	return s.upsertJobStatus(ctx, tx, run, repo, job)
}

func (s *WorkflowRunService) upsertJobStatus(ctx context.Context, tx *store.WorkflowRunTx, run *model.WorkflowRun, repo *model.Repository, job *model.WorkflowJob) error {
	creator, err := s.statusCreator(run, repo)
	if err != nil {
		return err
	}
	state, desc := commitStateOf(job.Status, run.Status == model.CIStatusWaiting)
	if job.Message != "" {
		desc += ": " + job.Message
	}
	runID := run.ID
	return tx.UpsertStatus(ctx, &model.CommitStatus{
		RepoID: repo.ID, SHA: run.SHA, Context: job.StatusContext, State: state, Description: desc,
		TargetURL: s.runURL(repo, run), CreatorID: creator, RunID: &runID,
	})
}

// commitStateOf maps a Job status onto the four commit status states. A cancelled Job is an error so a required
// check never passes by being cancelled; a skipped one passes, as on GitHub.
func commitStateOf(st model.CIStatus, awaitingApproval bool) (model.CommitStatusState, string) {
	switch st {
	case model.CIStatusSuccess:
		return model.CommitStatusSuccess, "Succeeded"
	case model.CIStatusSkipped:
		return model.CommitStatusSuccess, "Skipped"
	case model.CIStatusFailure:
		return model.CommitStatusFailure, "Failed"
	case model.CIStatusCancelled:
		return model.CommitStatusError, "Cancelled"
	case model.CIStatusRunning:
		return model.CommitStatusPending, "Running"
	}
	if awaitingApproval {
		return model.CommitStatusPending, "Approval required"
	}
	return model.CommitStatusPending, "Queued"
}

func (s *WorkflowRunService) runURL(repo *model.Repository, run *model.WorkflowRun) string {
	return fmt.Sprintf("%s/%s/%s/actions/runs/%d", strings.TrimRight(s.baseURL, "/"), repo.OwnerName, repo.Name, run.Number)
}

// statusCreator is the user a Run's statuses are attributed to: whoever triggered it, or the repo owner when no user
// did, as with a deploy-key push.
func (s *WorkflowRunService) statusCreator(run *model.WorkflowRun, repo *model.Repository) (int64, error) {
	switch {
	case run.TriggerUserID != nil:
		return *run.TriggerUserID, nil
	case repo.OwnerID != 0:
		return repo.OwnerID, nil
	case repo.CreatedBy != 0:
		return repo.CreatedBy, nil
	}
	return 0, fmt.Errorf("repo %d has no user to attribute workflow statuses to", repo.ID)
}

// cancelRun cancels every unfinished Job of the Run and then the Run itself.
func (s *WorkflowRunService) cancelRun(ctx context.Context, tx *store.WorkflowRunTx, run *model.WorkflowRun, repo *model.Repository, by *int64, msg string) error {
	jobs, err := tx.CurrentJobs(ctx, run.ID)
	if err != nil {
		return err
	}
	for i := range jobs {
		if jobs[i].Status.Finished() {
			continue
		}
		if err := s.setJobStatus(ctx, tx, run, repo, &jobs[i], model.CIStatusCancelled, msg); err != nil {
			return err
		}
	}
	if err := tx.SetRunCancelledBy(ctx, run.ID, by); err != nil {
		return err
	}
	run.Status = model.CIStatusCancelled
	if err := tx.UpdateRunState(ctx, run.ID, run.Status); err != nil {
		return err
	}
	slog.Debug("workflow run cancelled", "run_id", run.ID, "reason", msg)
	return nil
}
