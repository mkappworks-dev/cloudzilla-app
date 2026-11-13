package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

type workflowDB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// workflowQ holds the queries; the store runs them on the pool and WorkflowRunTx on one transaction.
type workflowQ struct{ q workflowDB }

// WorkflowRunStore persists workflow Runs, Jobs and Steps.
type WorkflowRunStore struct {
	workflowQ
	db *sql.DB
}

func NewWorkflowRunStore(db *sql.DB) *WorkflowRunStore {
	return &WorkflowRunStore{workflowQ: workflowQ{db}, db: db}
}

// WorkflowRunTx is the store's queries on one transaction.
type WorkflowRunTx struct {
	workflowQ
	afterCommit []func()
}

// AfterCommit runs fn once the transaction has committed, and never if it rolls back.
func (t *WorkflowRunTx) AfterCommit(fn func()) { t.afterCommit = append(t.afterCommit, fn) }

// InTx runs fn in a transaction, committing when it returns nil.
func (s *WorkflowRunStore) InTx(ctx context.Context, fn func(tx *WorkflowRunTx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("workflow tx begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck
	wtx := &WorkflowRunTx{workflowQ: workflowQ{tx}}
	if err := fn(wtx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for _, f := range wtx.afterCommit {
		f()
	}
	return nil
}

// UpsertStatus writes a commit status on the transaction, so it commits with the rows it reports.
func (t *WorkflowRunTx) UpsertStatus(ctx context.Context, cs *model.CommitStatus) error {
	return upsertCommitStatus(ctx, t.q, cs)
}

// NextNumber takes the repo's next Run number; the row lock serialises concurrent creators until commit.
func (t *WorkflowRunTx) NextNumber(ctx context.Context, repoID int64) (int, error) {
	var n int
	err := t.q.QueryRowContext(ctx,
		`INSERT INTO workflow_run_counters (repo_id, last_number) VALUES ($1, 1)
		 ON CONFLICT (repo_id) DO UPDATE SET last_number = workflow_run_counters.last_number + 1
		 RETURNING last_number`, repoID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("workflow run number: %w", err)
	}
	return n, nil
}

// LockRun takes the Run's row lock, so scheduling and cancelling it do not interleave.
func (t *WorkflowRunTx) LockRun(ctx context.Context, id int64) (*model.WorkflowRun, error) {
	return t.getRun(ctx, `WHERE id = $1 FOR UPDATE`, id)
}

func (q workflowQ) RepoCIEnabled(ctx context.Context, repoID int64) (bool, error) {
	var on bool
	err := q.q.QueryRowContext(ctx, `SELECT ci_enabled FROM repositories WHERE id = $1`, repoID).Scan(&on)
	if err != nil {
		return false, fmt.Errorf("repo ci_enabled: %w", err)
	}
	return on, nil
}

func (q workflowQ) SetRepoCIEnabled(ctx context.Context, repoID int64, on bool) error {
	_, err := q.q.ExecContext(ctx, `UPDATE repositories SET ci_enabled = $2 WHERE id = $1`, repoID, on)
	if err != nil {
		return fmt.Errorf("set repo ci_enabled: %w", err)
	}
	return nil
}

const runColumns = `id, repo_id, number, workflow_path, workflow_name, workflow_content, event, ref, sha, pull_request_id,
	attempt, status, no_secrets, concurrency_group, cancel_in_progress, trigger_user_id, cancelled_by, error,
	created_at, started_at, finished_at`

func scanRun(sc interface{ Scan(...any) error }) (*model.WorkflowRun, error) {
	var r model.WorkflowRun
	err := sc.Scan(&r.ID, &r.RepoID, &r.Number, &r.WorkflowPath, &r.WorkflowName, &r.WorkflowContent, &r.Event, &r.Ref,
		&r.SHA, &r.PullRequestID, &r.Attempt, &r.Status, &r.NoSecrets, &r.ConcurrencyGrp, &r.CancelInProg,
		&r.TriggerUserID, &r.CancelledBy, &r.Error, &r.CreatedAt, &r.StartedAt, &r.FinishedAt)
	return &r, err
}

func (q workflowQ) getRun(ctx context.Context, where string, args ...any) (*model.WorkflowRun, error) {
	r, err := scanRun(q.q.QueryRowContext(ctx, `SELECT `+runColumns+` FROM workflow_runs `+where, args...))
	if err != nil {
		return nil, fmt.Errorf("workflow run: %w", err)
	}
	return r, nil
}

func (q workflowQ) GetRun(ctx context.Context, id int64) (*model.WorkflowRun, error) {
	return q.getRun(ctx, `WHERE id = $1`, id)
}

func (q workflowQ) GetRunByNumber(ctx context.Context, repoID int64, number int) (*model.WorkflowRun, error) {
	return q.getRun(ctx, `WHERE repo_id = $1 AND number = $2`, repoID, number)
}

func (q workflowQ) queryRuns(ctx context.Context, where string, args ...any) ([]model.WorkflowRun, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT `+runColumns+` FROM workflow_runs `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("workflow runs: %w", err)
	}
	defer rows.Close()
	var out []model.WorkflowRun
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// ListRuns returns the repo's Runs, newest first.
func (q workflowQ) ListRuns(ctx context.Context, repoID int64, limit int) ([]model.WorkflowRun, error) {
	return q.queryRuns(ctx, `WHERE repo_id = $1 ORDER BY number DESC LIMIT $2`, repoID, limit)
}

// ListRunsFiltered returns one page of the repo's Runs, newest first, and whether more follow.
func (q workflowQ) ListRunsFiltered(ctx context.Context, repoID int64, f model.RunFilter, limit, offset int) ([]model.WorkflowRun, bool, error) {
	where := `WHERE repo_id = $1`
	args := []any{repoID}
	add := func(cond string, v any) {
		args = append(args, v)
		where += fmt.Sprintf(" AND "+cond, len(args))
	}
	if f.WorkflowPath != "" {
		add("workflow_path = $%d", f.WorkflowPath)
	}
	if f.Branch != "" {
		args = append(args, "refs/heads/"+f.Branch, "refs/tags/"+f.Branch)
		where += fmt.Sprintf(" AND ref IN ($%d, $%d)", len(args)-1, len(args))
	}
	if f.Status != "" {
		add("status = $%d", string(f.Status))
	}
	switch f.Event {
	case "":
	case "manual":
		add("event = $%d", model.RunEventDispatch)
	default:
		add("event = $%d", f.Event)
	}
	args = append(args, limit+1, offset)
	runs, err := q.queryRuns(ctx, where+fmt.Sprintf(` ORDER BY number DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, false, err
	}
	more := len(runs) > limit
	if more {
		runs = runs[:limit]
	}
	return runs, more, nil
}

// RunFacets lists the workflow files and branches or tags the repo's Runs came from.
func (q workflowQ) RunFacets(ctx context.Context, repoID int64) (model.RunFacets, error) {
	var out model.RunFacets
	rows, err := q.q.QueryContext(ctx, `SELECT DISTINCT ON (workflow_path) workflow_path, workflow_name FROM workflow_runs
		WHERE repo_id = $1 ORDER BY workflow_path, number DESC`, repoID)
	if err != nil {
		return out, fmt.Errorf("run facets: %w", err)
	}
	for rows.Next() {
		var w model.RunWorkflow
		if err := rows.Scan(&w.Path, &w.Name); err != nil {
			_ = rows.Close()
			return out, err
		}
		out.Workflows = append(out.Workflows, w)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return out, fmt.Errorf("run facets: %w", err)
	}
	_ = rows.Close()
	rows, err = q.q.QueryContext(ctx, `SELECT DISTINCT ref FROM workflow_runs WHERE repo_id = $1 ORDER BY ref`, repoID)
	if err != nil {
		return out, fmt.Errorf("run facets: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return out, err
		}
		out.Branches = append(out.Branches, strings.TrimPrefix(strings.TrimPrefix(ref, "refs/heads/"), "refs/tags/"))
	}
	return out, rows.Err()
}

// ActionsVisible reports whether the repo's Actions tab shows: CI is on for it, or it still has Runs to read.
func (q workflowQ) ActionsVisible(ctx context.Context, repoID int64) (bool, error) {
	var ok bool
	err := q.q.QueryRowContext(ctx, `SELECT ci_enabled OR EXISTS (SELECT 1 FROM workflow_runs WHERE repo_id = $1) FROM repositories WHERE id = $1`,
		repoID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("actions visible: %w", err)
	}
	return ok, nil
}

// ListRunsBySHA returns the Runs for one commit, oldest first.
func (q workflowQ) ListRunsBySHA(ctx context.Context, repoID int64, sha string) ([]model.WorkflowRun, error) {
	return q.queryRuns(ctx, `WHERE repo_id = $1 AND sha = $2 ORDER BY number`, repoID, sha)
}

// ListRunsByIDs returns the repo's Runs with the given IDs, newest first.
func (q workflowQ) ListRunsByIDs(ctx context.Context, repoID int64, ids []int64) ([]model.WorkflowRun, error) {
	return q.queryRuns(ctx, `WHERE repo_id = $1 AND id = ANY($2) ORDER BY number DESC`, repoID, ids)
}

// OpenRunsInGroup returns Runs not yet finished that share a concurrency group, except `except`.
func (q workflowQ) OpenRunsInGroup(ctx context.Context, repoID int64, group string, except int64) ([]model.WorkflowRun, error) {
	return q.queryRuns(ctx,
		`WHERE repo_id = $1 AND concurrency_group = $2 AND id <> $3 AND status IN ('queued', 'running') ORDER BY id`,
		repoID, group, except)
}

// QueuedRunsFor returns queued Runs of one workflow on one ref (and pull request), except `except`.
func (q workflowQ) QueuedRunsFor(ctx context.Context, repoID int64, workflowPath, ref string, pullID *int64, except int64) ([]model.WorkflowRun, error) {
	return q.queryRuns(ctx,
		`WHERE repo_id = $1 AND workflow_path = $2 AND ref = $3 AND pull_request_id IS NOT DISTINCT FROM $4
		   AND id <> $5 AND status = 'queued' ORDER BY id`,
		repoID, workflowPath, ref, pullID, except)
}

// QueuedRuns returns the repo's queued Runs, oldest first.
func (q workflowQ) QueuedRuns(ctx context.Context, repoID int64) ([]model.WorkflowRun, error) {
	return q.queryRuns(ctx, `WHERE repo_id = $1 AND status = 'queued' ORDER BY id`, repoID)
}

func (q workflowQ) InsertRun(ctx context.Context, r *model.WorkflowRun) error {
	err := q.q.QueryRowContext(ctx,
		`INSERT INTO workflow_runs (repo_id, number, workflow_path, workflow_name, workflow_content, event, ref, sha,
		     pull_request_id, attempt, status, no_secrets, concurrency_group, cancel_in_progress, trigger_user_id, error,
		     finished_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
		     CASE WHEN $11 IN ('success', 'failure', 'cancelled') THEN NOW() END)
		 RETURNING id, created_at, finished_at`,
		r.RepoID, r.Number, r.WorkflowPath, r.WorkflowName, r.WorkflowContent, r.Event, r.Ref, r.SHA, r.PullRequestID,
		r.Attempt, r.Status, r.NoSecrets, r.ConcurrencyGrp, r.CancelInProg, r.TriggerUserID, r.Error,
	).Scan(&r.ID, &r.CreatedAt, &r.FinishedAt)
	if err != nil {
		return fmt.Errorf("insert workflow run: %w", err)
	}
	return nil
}

// UpdateRunState sets a Run's status and its time stamps: started_at once, finished_at when final.
func (q workflowQ) UpdateRunState(ctx context.Context, id int64, status model.CIStatus) error {
	_, err := q.q.ExecContext(ctx,
		`UPDATE workflow_runs SET status = $2,
		     started_at = CASE WHEN $2 IN ('running', 'success', 'failure') AND started_at IS NULL THEN NOW() ELSE started_at END,
		     finished_at = CASE WHEN $2 IN ('success', 'failure', 'cancelled') THEN COALESCE(finished_at, NOW()) ELSE NULL END
		 WHERE id = $1`, id, status)
	if err != nil {
		return fmt.Errorf("update workflow run state: %w", err)
	}
	return nil
}

func (q workflowQ) SetRunCancelledBy(ctx context.Context, id int64, userID *int64) error {
	_, err := q.q.ExecContext(ctx, `UPDATE workflow_runs SET cancelled_by = $2 WHERE id = $1`, id, userID)
	if err != nil {
		return fmt.Errorf("set run cancelled_by: %w", err)
	}
	return nil
}

// BumpAttempt starts the next attempt of a finished Run.
func (q workflowQ) BumpAttempt(ctx context.Context, id int64) (int, error) {
	var attempt int
	err := q.q.QueryRowContext(ctx,
		`UPDATE workflow_runs SET attempt = attempt + 1, finished_at = NULL, cancelled_by = NULL WHERE id = $1
		 RETURNING attempt`, id).Scan(&attempt)
	if err != nil {
		return 0, fmt.Errorf("bump run attempt: %w", err)
	}
	return attempt, nil
}

// ApproveRun moves a waiting Run to queued; it reports false when the Run was not waiting.
func (q workflowQ) ApproveRun(ctx context.Context, id int64) (bool, error) {
	res, err := q.q.ExecContext(ctx, `UPDATE workflow_runs SET status = 'queued' WHERE id = $1 AND status = 'waiting'`, id)
	if err != nil {
		return false, fmt.Errorf("approve workflow run: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

const jobColumns = `id, run_id, attempt, job_key, name, status_context, matrix, needs, if_expr, runs_on, container, env,
	artifacts, timeout_minutes, status, message, runner_id, lease_until, heartbeat_at, created_at, queued_at, started_at,
	finished_at`

func scanJob(sc interface{ Scan(...any) error }) (*model.WorkflowJob, error) {
	var j model.WorkflowJob
	var matrix, needs, runsOn, env, artifacts []byte
	err := sc.Scan(&j.ID, &j.RunID, &j.Attempt, &j.JobKey, &j.Name, &j.StatusContext, &matrix, &needs, &j.IfExpr, &runsOn,
		&j.Container, &env, &artifacts, &j.TimeoutMins, &j.Status, &j.Message, &j.RunnerID, &j.LeaseUntil,
		&j.HeartbeatAt, &j.CreatedAt, &j.QueuedAt, &j.StartedAt, &j.FinishedAt)
	if err != nil {
		return nil, err
	}
	for _, f := range []struct {
		raw []byte
		to  any
	}{{matrix, &j.Matrix}, {needs, &j.Needs}, {runsOn, &j.RunsOn}, {env, &j.Env}, {artifacts, &j.Artifacts}} {
		if err := json.Unmarshal(f.raw, f.to); err != nil {
			return nil, fmt.Errorf("decode workflow job json: %w", err)
		}
	}
	return &j, nil
}

func (q workflowQ) InsertJob(ctx context.Context, j *model.WorkflowJob) error {
	enc := map[string][]byte{}
	for k, v := range map[string]any{"matrix": nonNilMap(j.Matrix), "needs": nonNilSlice(j.Needs), "runs_on": nonNilSlice(j.RunsOn),
		"env": nonNilMap(j.Env), "artifacts": nonNilArtifacts(j.Artifacts)} {
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("encode workflow job %s: %w", k, err)
		}
		enc[k] = b
	}
	err := q.q.QueryRowContext(ctx,
		`INSERT INTO workflow_jobs (run_id, attempt, job_key, name, status_context, matrix, needs, if_expr, runs_on,
		     container, env, artifacts, timeout_minutes, status, message)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		 RETURNING id, created_at`,
		j.RunID, j.Attempt, j.JobKey, j.Name, j.StatusContext, enc["matrix"], enc["needs"], j.IfExpr, enc["runs_on"],
		j.Container, enc["env"], enc["artifacts"], j.TimeoutMins, j.Status, j.Message,
	).Scan(&j.ID, &j.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert workflow job: %w", err)
	}
	return nil
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func nonNilSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nonNilArtifacts(a []model.WorkflowArtifactSpec) []model.WorkflowArtifactSpec {
	if a == nil {
		return []model.WorkflowArtifactSpec{}
	}
	return a
}

func (q workflowQ) InsertStep(ctx context.Context, s *model.WorkflowStep) error {
	env, err := json.Marshal(nonNilMap(s.Env))
	if err != nil {
		return fmt.Errorf("encode workflow step env: %w", err)
	}
	err = q.q.QueryRowContext(ctx,
		`INSERT INTO workflow_steps (job_id, position, name, run, shell, working_directory, env, if_expr, timeout_minutes, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
		s.JobID, s.Position, s.Name, s.Run, s.Shell, s.WorkingDir, env, s.IfExpr, s.TimeoutMin, s.Status,
	).Scan(&s.ID)
	if err != nil {
		return fmt.Errorf("insert workflow step: %w", err)
	}
	return nil
}

func (q workflowQ) queryJobs(ctx context.Context, where string, args ...any) ([]model.WorkflowJob, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT `+jobColumns+` FROM workflow_jobs j `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("workflow jobs: %w", err)
	}
	defer rows.Close()
	var out []model.WorkflowJob
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

// CurrentJobs returns each Job of a Run at its highest attempt, in creation order.
func (q workflowQ) CurrentJobs(ctx context.Context, runID int64) ([]model.WorkflowJob, error) {
	return q.queryJobs(ctx,
		`WHERE run_id = $1 AND attempt = (SELECT MAX(attempt) FROM workflow_jobs WHERE run_id = j.run_id AND name = j.name)
		 ORDER BY id`, runID)
}

// AllJobs returns every attempt of every Job of a Run.
func (q workflowQ) AllJobs(ctx context.Context, runID int64) ([]model.WorkflowJob, error) {
	return q.queryJobs(ctx, `WHERE run_id = $1 ORDER BY attempt, id`, runID)
}

func (q workflowQ) GetJob(ctx context.Context, id int64) (*model.WorkflowJob, error) {
	jobs, err := q.queryJobs(ctx, `WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, sql.ErrNoRows
	}
	return &jobs[0], nil
}

func (q workflowQ) JobSteps(ctx context.Context, jobID int64) ([]model.WorkflowStep, error) {
	rows, err := q.q.QueryContext(ctx,
		`SELECT id, job_id, position, name, run, shell, working_directory, env, if_expr, timeout_minutes, status,
		        started_at, finished_at, log_first_line, log_last_line
		 FROM workflow_steps WHERE job_id = $1 ORDER BY position`, jobID)
	if err != nil {
		return nil, fmt.Errorf("workflow steps: %w", err)
	}
	defer rows.Close()
	var out []model.WorkflowStep
	for rows.Next() {
		var s model.WorkflowStep
		var env []byte
		if err := rows.Scan(&s.ID, &s.JobID, &s.Position, &s.Name, &s.Run, &s.Shell, &s.WorkingDir, &env, &s.IfExpr,
			&s.TimeoutMin, &s.Status, &s.StartedAt, &s.FinishedAt, &s.FirstLine, &s.LastLine); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(env, &s.Env); err != nil {
			return nil, fmt.Errorf("decode workflow step env: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SetJobStatus moves a Job to a status and stamps the matching time.
func (q workflowQ) SetJobStatus(ctx context.Context, id int64, status model.CIStatus, message string) error {
	_, err := q.q.ExecContext(ctx,
		`UPDATE workflow_jobs SET status = $2, message = $3,
		     queued_at = CASE WHEN $2 = 'queued' THEN NOW() ELSE queued_at END,
		     finished_at = CASE WHEN $2 IN ('success', 'failure', 'cancelled', 'skipped') THEN NOW() ELSE finished_at END
		 WHERE id = $1`, id, status, message)
	if err != nil {
		return fmt.Errorf("set workflow job status: %w", err)
	}
	return nil
}

// FinishOpenSteps ends a Job's unfinished Steps with status.
func (q workflowQ) FinishOpenSteps(ctx context.Context, jobID int64, status model.CIStatus) error {
	_, err := q.q.ExecContext(ctx,
		`UPDATE workflow_steps SET status = $2, finished_at = NOW() WHERE job_id = $1 AND status IN ('queued', 'running')`,
		jobID, status)
	if err != nil {
		return fmt.Errorf("finish workflow steps: %w", err)
	}
	return nil
}

// ClaimQueuedJob leases the oldest queued Job whose runs-on labels the runner has all of, limited to repoID's Jobs
// when it is not nil. A Job of a Run that is not running yet is skipped while the repo already has maxRunning Runs
// running.
func (q workflowQ) ClaimQueuedJob(ctx context.Context, labels []string, runnerID, repoID *int64, lease time.Duration, maxRunning int) (*model.WorkflowJob, error) {
	have, err := json.Marshal(nonNilSlice(labels))
	if err != nil {
		return nil, fmt.Errorf("encode runner labels: %w", err)
	}
	row := q.q.QueryRowContext(ctx,
		`UPDATE workflow_jobs SET status = 'running', runner_id = $2, started_at = NOW(), heartbeat_at = NOW(),
		     lease_until = NOW() + make_interval(secs => $3)
		 WHERE id = (
		     SELECT j.id FROM workflow_jobs j JOIN workflow_runs r ON r.id = j.run_id
		     WHERE j.status = 'queued' AND j.runs_on <@ $1::jsonb AND ($5::bigint IS NULL OR r.repo_id = $5)
		       AND (r.status = 'running'
		            OR (SELECT COUNT(*) FROM workflow_runs x WHERE x.repo_id = r.repo_id AND x.status = 'running') < $4)
		     ORDER BY j.queued_at, j.id LIMIT 1 FOR UPDATE OF j SKIP LOCKED)
		 RETURNING `+jobColumns, have, runnerID, lease.Seconds(), maxRunning, repoID)
	j, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim workflow job: %w", err)
	}
	return j, nil
}

// StaleQueuedJobs returns Jobs that have been queued since before cutoff.
func (q workflowQ) StaleQueuedJobs(ctx context.Context, cutoff time.Time) ([]model.WorkflowJob, error) {
	return q.queryJobs(ctx, `WHERE status = 'queued' AND queued_at < $1 ORDER BY id`, cutoff)
}

// LostRunningJobs returns running Jobs whose lease ended before now.
func (q workflowQ) LostRunningJobs(ctx context.Context, now time.Time) ([]model.WorkflowJob, error) {
	return q.queryJobs(ctx, `WHERE status = 'running' AND lease_until < $1 ORDER BY id`, now)
}

// RenewLease records a heartbeat for a running Job held by runnerID and reports whether it still was.
func (q workflowQ) RenewLease(ctx context.Context, jobID, runnerID int64, lease time.Duration) (bool, error) {
	res, err := q.q.ExecContext(ctx,
		`UPDATE workflow_jobs SET heartbeat_at = NOW(), lease_until = NOW() + make_interval(secs => $3)
		 WHERE id = $1 AND runner_id = $2 AND status = 'running'`, jobID, runnerID, lease.Seconds())
	if err != nil {
		return false, fmt.Errorf("renew workflow job lease: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// SetJobTokenHash stores the hash of the per-job token minted at claim.
func (q workflowQ) SetJobTokenHash(ctx context.Context, jobID int64, hash string) error {
	if _, err := q.q.ExecContext(ctx, `UPDATE workflow_jobs SET token_hash = $2 WHERE id = $1`, jobID, hash); err != nil {
		return fmt.Errorf("set workflow job token: %w", err)
	}
	return nil
}

// RunningJobByTokenHash returns the running Job a per-job token hash belongs to, or nil.
func (q workflowQ) RunningJobByTokenHash(ctx context.Context, hash string) (*model.WorkflowJob, error) {
	jobs, err := q.queryJobs(ctx, `WHERE token_hash = $1 AND token_hash <> '' AND status = 'running'`, hash)
	if err != nil || len(jobs) == 0 {
		return nil, err
	}
	return &jobs[0], nil
}

// SetStepStatus moves one Step of a Job and stamps the matching times.
func (q workflowQ) SetStepStatus(ctx context.Context, jobID int64, position int, status model.CIStatus) (bool, error) {
	res, err := q.q.ExecContext(ctx,
		`UPDATE workflow_steps SET status = $3,
		     started_at = CASE WHEN $3 = 'running' THEN NOW() ELSE started_at END,
		     finished_at = CASE WHEN $3 IN ('success', 'failure', 'cancelled', 'skipped') THEN NOW() ELSE finished_at END
		 WHERE job_id = $1 AND position = $2`, jobID, position, status)
	if err != nil {
		return false, fmt.Errorf("set workflow step status: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}
