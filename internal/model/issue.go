package model

import "time"

// IssueState represents the open/closed state of an issue.
type IssueState string

const (
	IssueStateOpen   IssueState = "open"
	IssueStateClosed IssueState = "closed"
)

// Issue represents a repository issue.
type Issue struct {
	ID          int64      `db:"id"           json:"id"`
	RepoID      int64      `db:"repo_id"      json:"repo_id"`
	Number      int        `db:"number"       json:"number"`
	AuthorID    int64      `db:"author_id"    json:"author_id"`
	AuthorName  string     `db:"author_name"  json:"author_name,omitempty"`
	Title       string     `db:"title"        json:"title"`
	Body        string     `db:"body"         json:"body"`
	State       IssueState `db:"state"        json:"state"`
	Priority    *string    `db:"priority"     json:"priority,omitempty"`
	MilestoneID *int64     `db:"milestone_id" json:"milestone_id,omitempty"`
	Visibility  string     `db:"visibility"   json:"visibility"`
	CreatedAt   time.Time  `db:"created_at"   json:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at"   json:"updated_at"`
	ClosedAt    *time.Time `db:"closed_at"    json:"closed_at,omitempty"`
	IsPinned    bool       `db:"is_pinned"    json:"is_pinned"`
	IsLocked    bool       `db:"is_locked"    json:"is_locked"`
	LockedAt    *time.Time `db:"locked_at"    json:"locked_at,omitempty"`
}
