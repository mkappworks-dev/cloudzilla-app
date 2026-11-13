package model

import "time"

// CIStatus is the state of a Run, Job or Step. A Run is never skipped, a Step is never waiting.
type CIStatus string

const (
	CIStatusWaiting   CIStatus = "waiting"
	CIStatusQueued    CIStatus = "queued"
	CIStatusRunning   CIStatus = "running"
	CIStatusSuccess   CIStatus = "success"
	CIStatusFailure   CIStatus = "failure"
	CIStatusCancelled CIStatus = "cancelled"
	CIStatusSkipped   CIStatus = "skipped"
)

// Finished reports whether the status is final.
func (s CIStatus) Finished() bool {
	switch s {
	case CIStatusSuccess, CIStatusFailure, CIStatusCancelled, CIStatusSkipped:
		return true
	}
	return false
}

const (
	RunEventPush     = "push"
	RunEventPull     = "pull_request"
	RunEventDispatch = "workflow_dispatch"
)

type WorkflowRun struct {
	ID              int64      `db:"id"`
	RepoID          int64      `db:"repo_id"`
	Number          int        `db:"number"`
	WorkflowPath    string     `db:"workflow_path"`
	WorkflowName    string     `db:"workflow_name"`
	WorkflowContent string     `db:"workflow_content"`
	Event           string     `db:"event"`
	Ref             string     `db:"ref"`
	SHA             string     `db:"sha"`
	PullRequestID   *int64     `db:"pull_request_id"`
	Attempt         int        `db:"attempt"`
	Status          CIStatus   `db:"status"`
	NoSecrets       bool       `db:"no_secrets"`
	ConcurrencyGrp  string     `db:"concurrency_group"`
	CancelInProg    bool       `db:"cancel_in_progress"`
	TriggerUserID   *int64     `db:"trigger_user_id"`
	CancelledBy     *int64     `db:"cancelled_by"`
	Error           string     `db:"error"`
	CreatedAt       time.Time  `db:"created_at"`
	StartedAt       *time.Time `db:"started_at"`
	FinishedAt      *time.Time `db:"finished_at"`
}

type WorkflowJob struct {
	ID            int64             `db:"id"`
	RunID         int64             `db:"run_id"`
	Attempt       int               `db:"attempt"`
	JobKey        string            `db:"job_key"`
	Name          string            `db:"name"`
	StatusContext string            `db:"status_context"`
	Matrix        map[string]string `db:"matrix"`
	Needs         []string          `db:"needs"`
	IfExpr        string            `db:"if_expr"`
	RunsOn        []string          `db:"runs_on"`
	Container     string            `db:"container"`
	Env           map[string]string `db:"env"`
	Artifacts     []WorkflowArtifactSpec
	TimeoutMins   int        `db:"timeout_minutes"`
	Status        CIStatus   `db:"status"`
	Message       string     `db:"message"`
	RunnerID      *int64     `db:"runner_id"`
	LeaseUntil    *time.Time `db:"lease_until"`
	HeartbeatAt   *time.Time `db:"heartbeat_at"`
	CreatedAt     time.Time  `db:"created_at"`
	QueuedAt      *time.Time `db:"queued_at"`
	StartedAt     *time.Time `db:"started_at"`
	FinishedAt    *time.Time `db:"finished_at"`
}

// WorkflowArtifactSpec is a job-level artifacts entry.
type WorkflowArtifactSpec struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type WorkflowStep struct {
	ID         int64             `db:"id"`
	JobID      int64             `db:"job_id"`
	Position   int               `db:"position"`
	Name       string            `db:"name"`
	Run        string            `db:"run"`
	Shell      string            `db:"shell"`
	WorkingDir string            `db:"working_directory"`
	Env        map[string]string `db:"env"`
	IfExpr     string            `db:"if_expr"`
	TimeoutMin int               `db:"timeout_minutes"`
	Status     CIStatus          `db:"status"`
	StartedAt  *time.Time        `db:"started_at"`
	FinishedAt *time.Time        `db:"finished_at"`
	FirstLine  int               `db:"log_first_line"`
	LastLine   int               `db:"log_last_line"`
}

// RunFilter narrows the runs list. Empty fields match everything; Event "manual" is a workflow_dispatch Run.
type RunFilter struct {
	WorkflowPath string
	Branch       string
	Status       CIStatus
	Event        string
}

// RunFacets are the values the runs list can filter by: what the repo's Runs actually used.
type RunFacets struct {
	Workflows []RunWorkflow
	Branches  []string
}

type RunWorkflow struct{ Path, Name string }
