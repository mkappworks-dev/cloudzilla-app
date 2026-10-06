package model

import "time"

const (
	IssueEventClosed   = "closed"
	IssueEventReopened = "reopened"
)

// IssueEvent is an entry on an issue's timeline. A close made by a closing
// keyword names the PR it was merged with or the commit that was pushed.
type IssueEvent struct {
	ID        int64     `db:"id"         json:"id"`
	IssueID   int64     `db:"issue_id"   json:"issue_id"`
	ActorID   int64     `db:"actor_id"   json:"actor_id"`
	ActorName string    `db:"actor_name" json:"actor_name"`
	Type      string    `db:"event_type" json:"event_type"`
	PullID    *int64    `db:"pull_id"    json:"pull_id,omitempty"`
	CommitSHA string    `db:"commit_sha" json:"commit_sha,omitempty"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`

	// SourceRepoID is the repo the PR or commit belongs to. The rest are read
	// for rendering; SourceOwner is empty when that repo is gone.
	SourceRepoID *int64 `db:"source_repo_id" json:"source_repo_id,omitempty"`
	PullNumber   int    `db:"-"              json:"pull_number,omitempty"`
	SourceOwner  string `db:"-"              json:"source_owner,omitempty"`
	SourceRepo   string `db:"-"              json:"source_repo,omitempty"`
}
