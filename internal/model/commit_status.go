package model

import "time"

type CommitStatusState string

const (
	CommitStatusPending CommitStatusState = "pending"
	CommitStatusSuccess CommitStatusState = "success"
	CommitStatusFailure CommitStatusState = "failure"
	CommitStatusError   CommitStatusState = "error"
)

type CommitStatus struct {
	ID          int64             `db:"id"          json:"id"`
	RepoID      int64             `db:"repo_id"     json:"repo_id"`
	SHA         string            `db:"sha"         json:"sha"`
	Context     string            `db:"context"     json:"context"`
	State       CommitStatusState `db:"state"       json:"state"`
	TargetURL   string            `db:"target_url"  json:"target_url"`
	Description string            `db:"description" json:"description"`
	CreatorID   int64             `db:"creator_id"  json:"creator_id"`
	// RunID is the workflow Run that owns the status; nil for a status an outside system posted.
	RunID     *int64    `db:"run_id"      json:"run_id,omitempty"`
	CreatedAt time.Time `db:"created_at"  json:"created_at"`
	UpdatedAt time.Time `db:"updated_at"  json:"updated_at"`
}
