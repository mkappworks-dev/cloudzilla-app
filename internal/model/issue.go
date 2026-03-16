package model

import "time"

type IssueState string

const (
	IssueStateOpen   IssueState = "open"
	IssueStateClosed IssueState = "closed"
)

type Issue struct {
	ID         int64      `db:"id"          json:"id"`
	RepoID     int64      `db:"repo_id"     json:"repo_id"`
	Number     int        `db:"number"      json:"number"`
	AuthorID   int64      `db:"author_id"   json:"author_id"`
	AuthorName string     `db:"author_name" json:"author_name,omitempty"`
	Title      string     `db:"title"       json:"title"`
	Body       string     `db:"body"        json:"body"`
	State      IssueState `db:"state"       json:"state"`
	CreatedAt  time.Time  `db:"created_at"  json:"created_at"`
	UpdatedAt  time.Time  `db:"updated_at"  json:"updated_at"`
	ClosedAt   *time.Time `db:"closed_at"   json:"closed_at"`
}
