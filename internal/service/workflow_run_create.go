package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"path"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/protocol/packp"

	"github.com/mkappworks-dev/cloudzilla-app/internal/ci/workflow"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

var ErrCIDisabled = errors.New("CI is off for this repository")

// runTrigger is what started a Run.
type runTrigger struct {
	event  string
	ref    string
	sha    string
	action string
	pull   *model.PullRequest
	// userID is zero when no user acted, as with a deploy-key push.
	userID int64
	// fork marks a pull request from another repository: its Run waits for approval and gets no secrets.
	fork bool
	// source is the repository whose git holds sha; it is the head repository for a fork pull request.
	source *model.Repository
	// onlyPath limits the Run to one workflow file, for a dispatch.
	onlyPath string
}

// OnPush creates Runs for each branch or tag the push created or moved, and for the open pull requests whose head
// branch moved. Deleting refs creates none.
func (s *WorkflowRunService) OnPush(ctx context.Context, repo *model.Repository, userID int64, commands []*packp.Command) {
	if s.pushDone != nil {
		defer s.pushDone()
	}
	for _, cmd := range commands {
		name := cmd.Name.String()
		isBranch := strings.HasPrefix(name, "refs/heads/")
		if cmd.Action() == packp.Delete || (!isBranch && !strings.HasPrefix(name, "refs/tags/")) {
			continue
		}
		sha := cmd.New.String()
		_, err := s.createRuns(ctx, repo, runTrigger{event: model.RunEventPush, ref: name, sha: sha, userID: userID, source: repo})
		s.logCreate(repo, err)
		if isBranch {
			s.onBranchMoved(ctx, repo, userID, strings.TrimPrefix(name, "refs/heads/"), sha)
		}
	}
}

func (s *WorkflowRunService) onBranchMoved(ctx context.Context, repo *model.Repository, userID int64, branch, sha string) {
	pulls, err := s.pulls.ListOpen(ctx, repo.ID)
	if err != nil {
		slog.Error("workflow runs: list open pull requests failed", "repo_id", repo.ID, "error", err)
		return
	}
	for i := range pulls {
		if pulls[i].HeadBranch != branch {
			continue
		}
		pulls[i].HeadSHA = sha
		s.OnPullRequest(ctx, repo, nil, &pulls[i], "synchronize", userID)
	}
}

// OnPullRequest creates Runs for a pull request that was opened, reopened or got new commits (action "opened",
// "reopened" or "synchronize"). head is the head repository, or nil when it is base; a different one makes the Runs
// of a fork pull request, which wait for approval.
func (s *WorkflowRunService) OnPullRequest(ctx context.Context, base, head *model.Repository, pr *model.PullRequest, action string, userID int64) {
	source := base
	if head != nil && head.ID != base.ID {
		source = head
	}
	sha := pr.HeadSHA
	if sha == "" {
		c, _, err := s.code.ResolveRef(source.OwnerName, source.Name, pr.HeadBranch)
		if err != nil || c == nil {
			slog.Warn("workflow runs: resolve pull request head failed", "pull_id", pr.ID, "error", err)
			return
		}
		sha = c.Hash.String()
	}
	_, err := s.createRuns(ctx, base, runTrigger{
		event: model.RunEventPull, action: action, ref: fmt.Sprintf("refs/pull/%d/head", pr.Number), sha: sha, pull: pr,
		userID: userID, fork: source != base, source: source,
	})
	s.logCreate(base, err)
}

func (s *WorkflowRunService) logCreate(repo *model.Repository, err error) {
	if err != nil {
		slog.Error("workflow runs: create failed", "repo_id", repo.ID, "owner", repo.OwnerName, "repo", repo.Name, "error", err)
	}
}

// Dispatch starts the workflow file at workflowPath on ref, which must have a workflow_dispatch trigger.
func (s *WorkflowRunService) Dispatch(ctx context.Context, repo *model.Repository, userID int64, workflowPath, ref string) (*model.WorkflowRun, error) {
	if !s.repoSvc.CanWrite(ctx, repo, userID) {
		return nil, ErrWorkflowForbidden
	}
	commit, _, err := s.code.ResolveRef(repo.OwnerName, repo.Name, ref)
	if err != nil {
		return nil, err
	}
	full := "refs/tags/" + ref
	if s.code.HasBranch(repo.OwnerName, repo.Name, ref) {
		full = "refs/heads/" + ref
	}
	runs, err := s.createRuns(ctx, repo, runTrigger{
		event: model.RunEventDispatch, ref: full, sha: commit.Hash.String(), userID: userID, source: repo, onlyPath: workflowPath,
	})
	switch {
	case err != nil:
		return nil, err
	case len(runs) > 0:
		return runs[0], nil
	}
	if on, _ := s.runs.RepoCIEnabled(ctx, repo.ID); !on || !s.cfg.Enabled {
		return nil, ErrCIDisabled
	}
	return nil, ErrWorkflowNotFound
}

// createRuns makes one Run per workflow file at the trigger's commit that the trigger matches.
func (s *WorkflowRunService) createRuns(ctx context.Context, repo *model.Repository, t runTrigger) ([]*model.WorkflowRun, error) {
	repo, ok, err := s.eligible(ctx, repo)
	if err != nil || !ok {
		return nil, err
	}
	files, err := s.code.WorkflowFilesAt(t.source.OwnerName, t.source.Name, t.sha)
	if err != nil {
		return nil, fmt.Errorf("read workflows at %s: %w", t.sha, err)
	}
	var runs []*model.WorkflowRun
	for _, f := range files {
		if t.onlyPath != "" && f.Path != t.onlyPath {
			continue
		}
		run, err := s.createRun(ctx, repo, t, f)
		if err != nil {
			return runs, fmt.Errorf("create run for %s: %w", f.Path, err)
		}
		if run != nil {
			runs = append(runs, run)
		}
	}
	return runs, nil
}

// eligible reloads the repo, so a deleted, archived or CI-off repo creates nothing whatever the caller held.
func (s *WorkflowRunService) eligible(ctx context.Context, repo *model.Repository) (*model.Repository, bool, error) {
	if !s.cfg.Enabled {
		return nil, false, nil
	}
	fresh, err := s.repos.GetByID(ctx, repo.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if fresh.IsArchived {
		return nil, false, nil
	}
	on, err := s.runs.RepoCIEnabled(ctx, repo.ID)
	return fresh, on, err
}

// requireEligible gates the paths that start Jobs for an existing Run, which would otherwise hand out secrets in a
// repository that may no longer run CI.
func (s *WorkflowRunService) requireEligible(ctx context.Context, repo *model.Repository) error {
	_, ok, err := s.eligible(ctx, repo)
	if err != nil {
		return err
	}
	if !ok {
		return ErrCIDisabled
	}
	return nil
}

func (s *WorkflowRunService) limits() workflow.Limits {
	return workflow.Limits{MaxJobs: s.cfg.MaxJobsPerRun, MaxSteps: s.cfg.MaxStepsPerRun, MaxJobMinutes: s.cfg.MaxJobMinutes}
}

// parseWorkflow returns the parsed file, or the error text to show on a failed Run.
func (s *WorkflowRunService) parseWorkflow(file string, content []byte, readErr string) (*workflow.Workflow, string) {
	if readErr != "" {
		return nil, file + ": " + readErr
	}
	wf, errs := workflow.ParseWithLimits(file, content, s.limits())
	if len(errs) == 0 {
		return wf, ""
	}
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Error()
	}
	return nil, strings.Join(msgs, "\n")
}

func (s *WorkflowRunService) createRun(ctx context.Context, repo *model.Repository, t runTrigger, f WorkflowFile) (*model.WorkflowRun, error) {
	wf, parseErr := s.parseWorkflow(f.Path, f.Content, f.Err)
	run := &model.WorkflowRun{
		RepoID: repo.ID, WorkflowPath: f.Path, WorkflowName: workflowStem(f.Path), WorkflowContent: string(f.Content),
		Event: t.event, Ref: t.ref, SHA: t.sha, Attempt: 1, Status: model.CIStatusQueued, NoSecrets: t.fork, Error: parseErr,
	}
	if t.pull != nil {
		run.PullRequestID = &t.pull.ID
	}
	if t.userID != 0 {
		run.TriggerUserID = &t.userID
	}
	switch {
	case parseErr != "":
		run.Status = model.CIStatusFailure
	case !wf.On.Matches(workflow.Event{Name: t.event, Ref: t.ref, Action: t.action}):
		return nil, nil
	default:
		run.WorkflowName = wf.Name
		if t.fork {
			run.Status = model.CIStatusWaiting
		}
		if wf.Concurrency != nil {
			run.CancelInProg = wf.Concurrency.CancelInProgress
			run.ConcurrencyGrp = s.concurrencyGroup(ctx, run, repo, wf)
		}
	}
	err := s.runs.InTx(ctx, func(tx *store.WorkflowRunTx) error {
		return s.persistRun(ctx, tx, repo, run, wf)
	})
	return run, err
}

func (s *WorkflowRunService) concurrencyGroup(ctx context.Context, run *model.WorkflowRun, repo *model.Repository, wf *workflow.Workflow) string {
	group, err := workflow.Interpolate(wf.Concurrency.Group, workflow.Context{GitHub: s.githubContext(ctx, run, repo), Env: wf.Env})
	if err != nil {
		return wf.Concurrency.Group
	}
	return group
}

func workflowStem(p string) string {
	return strings.TrimSuffix(path.Base(p), path.Ext(p))
}

func (s *WorkflowRunService) persistRun(ctx context.Context, tx *store.WorkflowRunTx, repo *model.Repository, run *model.WorkflowRun, wf *workflow.Workflow) error {
	n, err := tx.NextNumber(ctx, repo.ID)
	if err != nil {
		return err
	}
	run.Number = n
	if err := tx.InsertRun(ctx, run); err != nil {
		return err
	}
	if wf == nil {
		s.notifyCompleted(tx, run)
		return s.upsertParseFailure(ctx, tx, repo, run)
	}
	if err := s.insertJobs(ctx, tx, repo, run, wf, nil); err != nil {
		return err
	}
	if err := s.advance(ctx, tx, run, repo); err != nil {
		return err
	}
	return s.applyConcurrency(ctx, tx, repo, run)
}

// upsertParseFailure posts the one failing status a broken workflow file gets, named for the file.
func (s *WorkflowRunService) upsertParseFailure(ctx context.Context, tx *store.WorkflowRunTx, repo *model.Repository, run *model.WorkflowRun) error {
	creator, err := s.statusCreator(run, repo)
	if err != nil {
		return err
	}
	desc, _, _ := strings.Cut(run.Error, "\n")
	runID := run.ID
	return tx.UpsertStatus(ctx, &model.CommitStatus{
		RepoID: repo.ID, SHA: run.SHA, Context: "ci/" + run.WorkflowName, State: model.CommitStatusFailure, Description: desc,
		TargetURL: s.runURL(repo, run), CreatorID: creator, RunID: &runID,
	})
}

// insertJobs writes the Jobs of wf for the Run's current attempt, in waiting status; only limits them to the named
// Jobs when it is not nil.
func (s *WorkflowRunService) insertJobs(ctx context.Context, tx *store.WorkflowRunTx, repo *model.Repository, run *model.WorkflowRun, wf *workflow.Workflow, only map[string]bool) error {
	for _, j := range wf.Jobs {
		if only != nil && !only[j.Name] {
			continue
		}
		row := jobRow(run, wf, j)
		if err := tx.InsertJob(ctx, row); err != nil {
			return err
		}
		for i, st := range j.Steps {
			if err := tx.InsertStep(ctx, stepRow(row.ID, i, st)); err != nil {
				return err
			}
		}
		if err := s.upsertJobStatus(ctx, tx, run, repo, row); err != nil {
			return err
		}
	}
	return nil
}

func jobRow(run *model.WorkflowRun, wf *workflow.Workflow, j *workflow.Job) *model.WorkflowJob {
	env := maps.Clone(wf.Env)
	if env == nil {
		env = map[string]string{}
	}
	maps.Copy(env, j.Env)
	row := &model.WorkflowJob{
		RunID: run.ID, Attempt: run.Attempt, JobKey: j.ID, Name: j.Name, StatusContext: statusContext(j), Matrix: j.Matrix,
		Needs: j.Needs, RunsOn: j.RunsOn, Container: j.Container, Env: env, TimeoutMins: j.TimeoutMinutes,
		Status: model.CIStatusWaiting,
	}
	if j.If != nil {
		row.IfExpr = j.If.String()
	}
	for _, a := range j.Artifacts {
		row.Artifacts = append(row.Artifacts, model.WorkflowArtifactSpec{Name: a.Name, Path: a.Path})
	}
	return row
}

// statusContext is ci/<job id>, with the matrix values appended for one combination of a matrix.
func statusContext(j *workflow.Job) string {
	if len(j.Matrix) == 0 {
		return "ci/" + j.ID
	}
	if i := strings.LastIndex(j.Name, " ("); i >= 0 {
		return "ci/" + j.ID + j.Name[i:]
	}
	return "ci/" + j.Name
}

func stepRow(jobID int64, pos int, st workflow.Step) *model.WorkflowStep {
	name := st.Name
	if name == "" {
		first, _, _ := strings.Cut(strings.TrimSpace(st.Run), "\n")
		name = "Run " + first
	}
	row := &model.WorkflowStep{
		JobID: jobID, Position: pos, Name: name, Run: st.Run, Shell: st.Shell, WorkingDir: st.WorkingDirectory, Env: st.Env,
		TimeoutMin: st.TimeoutMinutes, Status: model.CIStatusQueued,
	}
	if st.If != nil {
		row.IfExpr = st.If.String()
	}
	return row
}

// applyConcurrency cancels the Runs the new Run supersedes, then the oldest queued Runs over the repo's cap.
func (s *WorkflowRunService) applyConcurrency(ctx context.Context, tx *store.WorkflowRunTx, repo *model.Repository, run *model.WorkflowRun) error {
	if run.Status == model.CIStatusWaiting {
		return nil
	}
	var older []model.WorkflowRun
	if run.ConcurrencyGrp != "" && run.CancelInProg {
		g, err := tx.OpenRunsInGroup(ctx, repo.ID, run.ConcurrencyGrp, run.ID)
		if err != nil {
			return err
		}
		older = append(older, g...)
	}
	if run.Event != model.RunEventDispatch {
		q, err := tx.QueuedRunsFor(ctx, repo.ID, run.WorkflowPath, run.Ref, run.PullRequestID, run.ID)
		if err != nil {
			return err
		}
		older = append(older, q...)
	}
	msg := fmt.Sprintf("Superseded by run #%d", run.Number)
	if err := s.cancelAll(ctx, tx, repo, older, msg); err != nil {
		return err
	}
	queued, err := tx.QueuedRuns(ctx, repo.ID)
	if err != nil {
		return err
	}
	if over := len(queued) - s.cfg.MaxQueuedRunsPerRepo; over > 0 {
		return s.cancelAll(ctx, tx, repo, queued[:over], "Run queue is full")
	}
	return nil
}

// cancelAll cancels each Run that is still unfinished once its lock is held.
func (s *WorkflowRunService) cancelAll(ctx context.Context, tx *store.WorkflowRunTx, repo *model.Repository, runs []model.WorkflowRun, msg string) error {
	done := map[int64]bool{}
	for _, r := range runs {
		if done[r.ID] {
			continue
		}
		done[r.ID] = true
		locked, err := tx.LockRun(ctx, r.ID)
		if err != nil {
			return err
		}
		if locked.Status.Finished() {
			continue
		}
		if err := s.cancelRun(ctx, tx, locked, repo, nil, msg); err != nil {
			return err
		}
	}
	return nil
}

// Rerun starts the next attempt of a finished Run from its stored workflow file and SHA: every Job, or with failedOnly
// the Jobs that failed or were cancelled and the Jobs that need them. The earlier attempt's rows are kept.
func (s *WorkflowRunService) Rerun(ctx context.Context, repo *model.Repository, number int, userID int64, failedOnly bool) error {
	if !s.repoSvc.CanWrite(ctx, repo, userID) {
		return ErrWorkflowForbidden
	}
	if err := s.requireEligible(ctx, repo); err != nil {
		return err
	}
	return s.mutateRun(ctx, repo, number, func(tx *store.WorkflowRunTx, run *model.WorkflowRun) error {
		if !run.Status.Finished() {
			return ErrRunNotFinished
		}
		wf, parseErr := s.parseWorkflow(run.WorkflowPath, []byte(run.WorkflowContent), "")
		if parseErr != "" {
			return ErrRunNotRerunnable
		}
		current, err := tx.CurrentJobs(ctx, run.ID)
		if err != nil {
			return err
		}
		redo := rerunSet(current, failedOnly)
		if len(redo) == 0 {
			return ErrNothingToRerun
		}
		if run.Attempt, err = tx.BumpAttempt(ctx, run.ID); err != nil {
			return err
		}
		if err := s.insertJobs(ctx, tx, repo, run, wf, redo); err != nil {
			return err
		}
		run.Status = model.CIStatusQueued
		if err := tx.UpdateRunState(ctx, run.ID, run.Status); err != nil {
			return err
		}
		return s.advance(ctx, tx, run, repo)
	})
}

// rerunSet names the Jobs to run again. With failedOnly it is the failed or cancelled Jobs plus, transitively, every
// Job that needs one of them, since those were skipped or never ran on the broken result.
func rerunSet(current []model.WorkflowJob, failedOnly bool) map[string]bool {
	redo := map[string]bool{}
	keys := map[string]bool{}
	for _, j := range current {
		if !failedOnly || j.Status == model.CIStatusFailure || j.Status == model.CIStatusCancelled {
			redo[j.Name], keys[j.JobKey] = true, true
		}
	}
	for grew := failedOnly && len(redo) > 0; grew; {
		grew = false
		for _, j := range current {
			if redo[j.Name] {
				continue
			}
			for _, n := range j.Needs {
				if keys[n] {
					redo[j.Name], keys[j.JobKey], grew = true, true, true
					break
				}
			}
		}
	}
	return redo
}
