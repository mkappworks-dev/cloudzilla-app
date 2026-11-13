package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

var (
	ErrRunNotWaiting      = errors.New("run is not waiting for approval")
	ErrRunNotFinished     = errors.New("run has not finished")
	ErrRunFinished        = errors.New("run has already finished")
	ErrNothingToRerun     = errors.New("no jobs to re-run")
	ErrRunNotRerunnable   = errors.New("workflow file of this run cannot be parsed")
	ErrWorkflowNotFound   = errors.New("workflow not found at this ref")
	ErrWorkflowNoDispatch = errors.New("workflow does not have a workflow_dispatch trigger")
	ErrWorkflowForbidden  = errors.New("forbidden: writing to this repository is required")
	ErrStepNotFound       = errors.New("step not found")
)

// noRunnerTimeout is how long a Job may wait in the queue before it is cancelled as having no runner.
const noRunnerTimeout = 24 * time.Hour

// jobLease is how long a claimed Job is the claiming runner's before it counts as lost; the runner API renews it.
const jobLease = 60 * time.Second

// WorkflowRunService creates workflow Runs and moves their Jobs through the scheduler. The runner API calls only
// ClaimJob and FinishJob.
type WorkflowRunService struct {
	runs    *store.WorkflowRunStore
	repos   *store.RepoStore
	users   *store.UserStore
	pulls   *store.PullStore
	repoSvc *RepoService
	code    *CodeService
	cfg     config.CIConfig
	baseURL string
	// pushDone lets a test wait for OnPush, which AfterPush runs in the background.
	pushDone func()
}

// WithPushDone calls fn when OnPush has handled a push.
func (s *WorkflowRunService) WithPushDone(fn func()) *WorkflowRunService {
	s.pushDone = fn
	return s
}

func NewWorkflowRunService(runs *store.WorkflowRunStore, repos *store.RepoStore, users *store.UserStore, pulls *store.PullStore,
	repoSvc *RepoService, code *CodeService, cfg config.CIConfig, baseURL string) *WorkflowRunService {
	return &WorkflowRunService{runs: runs, repos: repos, users: users, pulls: pulls, repoSvc: repoSvc, code: code, cfg: cfg, baseURL: baseURL}
}

func (s *WorkflowRunService) GetRun(ctx context.Context, repoID int64, number int) (*model.WorkflowRun, error) {
	return s.runs.GetRunByNumber(ctx, repoID, number)
}

func (s *WorkflowRunService) ListRuns(ctx context.Context, repoID int64, limit int) ([]model.WorkflowRun, error) {
	return s.runs.ListRuns(ctx, repoID, limit)
}

// RunsByIDs returns the repo's Runs with the given IDs, newest first.
func (s *WorkflowRunService) RunsByIDs(ctx context.Context, repoID int64, ids []int64) ([]model.WorkflowRun, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return s.runs.ListRunsByIDs(ctx, repoID, ids)
}

// ListJobs returns the Run's Jobs at their latest attempt.
func (s *WorkflowRunService) ListJobs(ctx context.Context, runID int64) ([]model.WorkflowJob, error) {
	return s.runs.CurrentJobs(ctx, runID)
}

// ListAllJobs returns every attempt of every Job of the Run.
func (s *WorkflowRunService) ListAllJobs(ctx context.Context, runID int64) ([]model.WorkflowJob, error) {
	return s.runs.AllJobs(ctx, runID)
}

func (s *WorkflowRunService) ListSteps(ctx context.Context, jobID int64) ([]model.WorkflowStep, error) {
	return s.runs.JobSteps(ctx, jobID)
}

// SetRepoEnabled switches CI on or off for one repo; history stays.
func (s *WorkflowRunService) SetRepoEnabled(ctx context.Context, repoID int64, on bool) error {
	return s.runs.SetRepoCIEnabled(ctx, repoID, on)
}

// Approve queues a fork pull request's waiting Run.
func (s *WorkflowRunService) Approve(ctx context.Context, repo *model.Repository, number int, userID int64) error {
	if !s.repoSvc.CanWrite(ctx, repo, userID) {
		return ErrWorkflowForbidden
	}
	return s.mutateRun(ctx, repo, number, func(tx *store.WorkflowRunTx, run *model.WorkflowRun) error {
		ok, err := tx.ApproveRun(ctx, run.ID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrRunNotWaiting
		}
		run.Status = model.CIStatusQueued
		return s.advance(ctx, tx, run, repo)
	})
}

// Cancel ends a Run that has not finished and every Job of it that has not.
func (s *WorkflowRunService) Cancel(ctx context.Context, repo *model.Repository, number int, userID int64) error {
	if !s.repoSvc.CanWrite(ctx, repo, userID) {
		return ErrWorkflowForbidden
	}
	return s.mutateRun(ctx, repo, number, func(tx *store.WorkflowRunTx, run *model.WorkflowRun) error {
		if run.Status.Finished() {
			return ErrRunFinished
		}
		return s.cancelRun(ctx, tx, run, repo, &userID, "")
	})
}

// mutateRun runs fn on the locked Run, in one transaction.
func (s *WorkflowRunService) mutateRun(ctx context.Context, repo *model.Repository, number int, fn func(*store.WorkflowRunTx, *model.WorkflowRun) error) error {
	found, err := s.runs.GetRunByNumber(ctx, repo.ID, number)
	if err != nil {
		return err
	}
	return s.runs.InTx(ctx, func(tx *store.WorkflowRunTx) error {
		run, err := tx.LockRun(ctx, found.ID)
		if err != nil {
			return err
		}
		return fn(tx, run)
	})
}

// SweepUnclaimed cancels Jobs that have waited for a runner for over 24 hours and returns how many.
func (s *WorkflowRunService) SweepUnclaimed(ctx context.Context, now time.Time) (int, error) {
	stale, err := s.runs.StaleQueuedJobs(ctx, now.Add(-noRunnerTimeout))
	if err != nil {
		return 0, err
	}
	n := 0
	for _, j := range stale {
		cancelled, err := s.cancelStaleJob(ctx, j.ID)
		if err != nil {
			return n, err
		}
		if cancelled {
			n++
		}
	}
	return n, nil
}

func (s *WorkflowRunService) cancelStaleJob(ctx context.Context, jobID int64) (bool, error) {
	job, err := s.runs.GetJob(ctx, jobID)
	if err != nil {
		return false, err
	}
	cancelled := false
	err = s.runs.InTx(ctx, func(tx *store.WorkflowRunTx) error {
		run, err := tx.LockRun(ctx, job.RunID)
		if err != nil {
			return err
		}
		fresh, err := tx.GetJob(ctx, jobID)
		if err != nil || fresh.Status != model.CIStatusQueued {
			return err
		}
		repo, err := s.repos.GetByID(ctx, run.RepoID)
		if err != nil {
			return err
		}
		if err := s.setJobStatus(ctx, tx, run, repo, fresh, model.CIStatusCancelled, "no runner"); err != nil {
			return err
		}
		cancelled = true
		return s.advance(ctx, tx, run, repo)
	})
	return cancelled, err
}

// Run sweeps unclaimed Jobs hourly until ctx ends.
func (s *WorkflowRunService) Run(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := s.SweepUnclaimed(ctx, time.Now()); err != nil {
				slog.Error("workflow run sweep failed", "error", err)
			} else if n > 0 {
				slog.Info("workflow run sweep cancelled jobs with no runner", "jobs", n)
			}
		}
	}
}

// ClaimJob leases the oldest queued Job whose runs-on labels the runner holds, and starts its Run. It returns nil
// when nothing is claimable.
func (s *WorkflowRunService) ClaimJob(ctx context.Context, labels []string, runnerID *int64) (*model.WorkflowJob, error) {
	return s.ClaimJobFor(ctx, labels, runnerID, nil, "")
}

// ClaimJobFor is ClaimJob limited to one repo's Jobs when repoID is not nil, and it stores tokenHash, the hash of the
// per-job token, on the claimed Job in the same transaction.
func (s *WorkflowRunService) ClaimJobFor(ctx context.Context, labels []string, runnerID, repoID *int64, tokenHash string) (*model.WorkflowJob, error) {
	var claimed *model.WorkflowJob
	err := s.runs.InTx(ctx, func(tx *store.WorkflowRunTx) error {
		job, err := tx.ClaimQueuedJob(ctx, labels, runnerID, repoID, jobLease, s.cfg.MaxConcurrentRunsPerRepo)
		if err != nil || job == nil {
			return err
		}
		if tokenHash != "" {
			if err := tx.SetJobTokenHash(ctx, job.ID, tokenHash); err != nil {
				return err
			}
		}
		run, err := tx.GetRun(ctx, job.RunID)
		if err != nil {
			return err
		}
		repo, err := s.repos.GetByID(ctx, run.RepoID)
		if err != nil {
			return err
		}
		if err := s.upsertJobStatus(ctx, tx, run, repo, job); err != nil {
			return err
		}
		claimed = job
		return s.advance(ctx, tx, run, repo)
	})
	return claimed, err
}

// GetJob returns one Job by id.
func (s *WorkflowRunService) GetJob(ctx context.Context, id int64) (*model.WorkflowJob, error) {
	return s.runs.GetJob(ctx, id)
}

// GetRunByID returns one Run by id.
func (s *WorkflowRunService) GetRunByID(ctx context.Context, id int64) (*model.WorkflowRun, error) {
	return s.runs.GetRun(ctx, id)
}

// RenewLease records a heartbeat from the runner holding the Job and reports whether it still holds a running one;
// false tells the runner to stop, because the Job was cancelled, finished or taken away.
func (s *WorkflowRunService) RenewLease(ctx context.Context, jobID, runnerID int64) (bool, error) {
	return s.runs.RenewLease(ctx, jobID, runnerID, jobLease)
}

// SetStepStatus records a Step's progress while its Job is running.
func (s *WorkflowRunService) SetStepStatus(ctx context.Context, jobID int64, position int, status model.CIStatus) error {
	if status != model.CIStatusRunning && status != model.CIStatusSuccess && status != model.CIStatusFailure && status != model.CIStatusSkipped {
		return fmt.Errorf("a step cannot move to %q", status)
	}
	ok, err := s.runs.SetStepStatus(ctx, jobID, position, status)
	if err != nil {
		return err
	}
	if !ok {
		return ErrStepNotFound
	}
	return nil
}

// SweepLostJobs fails every running Job whose runner stopped heartbeating, which fails its Run too, and returns how
// many.
func (s *WorkflowRunService) SweepLostJobs(ctx context.Context, now time.Time) (int, error) {
	lost, err := s.runs.LostRunningJobs(ctx, now)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, j := range lost {
		switch err := s.FinishJob(ctx, j.ID, model.CIStatusFailure, "runner lost"); {
		case err == nil:
			n++
		case errors.Is(err, ErrRunFinished):
		default:
			return n, err
		}
	}
	return n, nil
}

// FinishJob records the result of a claimed Job: success, failure or cancelled.
func (s *WorkflowRunService) FinishJob(ctx context.Context, jobID int64, result model.CIStatus, message string) error {
	if result != model.CIStatusSuccess && result != model.CIStatusFailure && result != model.CIStatusCancelled {
		return fmt.Errorf("a job cannot finish as %q", result)
	}
	job, err := s.runs.GetJob(ctx, jobID)
	if err != nil {
		return err
	}
	return s.runs.InTx(ctx, func(tx *store.WorkflowRunTx) error {
		run, err := tx.LockRun(ctx, job.RunID)
		if err != nil {
			return err
		}
		fresh, err := tx.GetJob(ctx, jobID)
		if err != nil {
			return err
		}
		if fresh.Status.Finished() {
			return ErrRunFinished
		}
		repo, err := s.repos.GetByID(ctx, run.RepoID)
		if err != nil {
			return err
		}
		if err := s.setJobStatus(ctx, tx, run, repo, fresh, result, message); err != nil {
			return err
		}
		return s.advance(ctx, tx, run, repo)
	})
}
