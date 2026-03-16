package model

import "time"

type Comment struct {
	ID         int64     `db:"id"          json:"id"`
	RepoID     int64     `db:"repo_id"     json:"repo_id"`
	IssueID    *int64    `db:"issue_id"    json:"issue_id"`
	PullID     *int64    `db:"pull_id"     json:"pull_id"`
	AuthorID   int64     `db:"author_id"   json:"author_id"`
	AuthorName string    `db:"author_name" json:"author_name,omitempty"`
	Body       string    `db:"body"        json:"body"`
	CreatedAt  time.Time `db:"created_at"  json:"created_at"`
	UpdatedAt  time.Time `db:"updated_at"  json:"updated_at"`
}
