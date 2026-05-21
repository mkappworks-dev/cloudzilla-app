package model

import "time"

type PRState string

const (
	PRStateOpen   PRState = "open"
	PRStateClosed PRState = "closed"
	PRStateMerged PRState = "merged"
)

type PullRequest struct {
	ID          int64      `db:"id"            json:"id"`
	RepoID      int64      `db:"repo_id"       json:"repo_id"`
	Number      int        `db:"number"        json:"number"`
	AuthorID    int64      `db:"author_id"     json:"author_id"`
	AuthorName  string     `db:"author_name"   json:"author_name,omitempty"`
	Title       string     `db:"title"         json:"title"`
	Body        string     `db:"body"          json:"body"`
	State       PRState    `db:"state"         json:"state"`
	HeadBranch  string     `db:"head_branch"   json:"head_branch"`
	BaseBranch  string     `db:"base_branch"   json:"base_branch"`
	MilestoneID *int64     `db:"milestone_id"  json:"milestone_id,omitempty"`
	CreatedAt   time.Time  `db:"created_at"    json:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at"    json:"updated_at"`
	MergedAt    *time.Time `db:"merged_at"     json:"merged_at"`
	ClosedAt    *time.Time `db:"closed_at"     json:"closed_at"`
	IsDraft           bool       `db:"is_draft"             json:"is_draft"`
	DraftAt           *time.Time `db:"draft_at"             json:"draft_at,omitempty"`
	AutoMergeEnabled  bool       `db:"auto_merge_enabled"   json:"auto_merge_enabled"`
	AutoMergeStrategy string     `db:"auto_merge_strategy"  json:"auto_merge_strategy,omitempty"`
	HeadSHA           string     `db:"head_sha"             json:"head_sha,omitempty"`
}
