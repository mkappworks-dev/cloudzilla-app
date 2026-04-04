package model

import "time"

type Project struct {
	ID          int64     `db:"id"          json:"id"`
	RepoID      int64     `db:"repo_id"     json:"repo_id"`
	Name        string    `db:"name"        json:"name"`
	Description string    `db:"description" json:"description"`
	CreatedAt   time.Time `db:"created_at"  json:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"  json:"updated_at"`
}

type ProjectColumn struct {
	ID        int64     `db:"id"         json:"id"`
	ProjectID int64     `db:"project_id" json:"project_id"`
	Name      string    `db:"name"       json:"name"`
	Position  int       `db:"position"   json:"position"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

// ProjectCard is a card in a column. IssueID / PullID / Note are mutually
// exclusive per the DB CHECK constraint. IssueTitle, IssueState, PullTitle,
// and PullState are populated via JOIN when listing cards.
type ProjectCard struct {
	ID        int64     `db:"id"          json:"id"`
	ColumnID  int64     `db:"column_id"   json:"column_id"`
	IssueID   *int64    `db:"issue_id"    json:"issue_id,omitempty"`
	PullID    *int64    `db:"pull_id"     json:"pull_id,omitempty"`
	Note      string    `db:"note"        json:"note"`
	Position  int       `db:"position"    json:"position"`
	CreatedAt time.Time `db:"created_at"  json:"created_at"`

	// Populated via JOIN — not stored columns
	IssueTitle  string `db:"issue_title"  json:"issue_title,omitempty"`
	IssueNumber int    `db:"issue_number" json:"issue_number,omitempty"`
	IssueState  string `db:"issue_state"  json:"issue_state,omitempty"`
	PullTitle   string `db:"pull_title"   json:"pull_title,omitempty"`
	PullNumber  int    `db:"pull_number"  json:"pull_number,omitempty"`
	PullState   string `db:"pull_state"   json:"pull_state,omitempty"`
}
