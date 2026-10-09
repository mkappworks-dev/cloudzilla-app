package model

import "time"

// Project represents a Kanban project board associated with a repository.
// ClosedAt is nil while the board is open.
type Project struct {
	ID          int64      `db:"id"          json:"id"`
	RepoID      int64      `db:"repo_id"     json:"repo_id"`
	Name        string     `db:"name"        json:"name"`
	Description string     `db:"description" json:"description"`
	CreatedAt   time.Time  `db:"created_at"  json:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at"  json:"updated_at"`
	ClosedAt    *time.Time `db:"closed_at"   json:"closed_at,omitempty"`
}

// ProjectColumn represents a column (e.g. To Do, In Progress) within a project board.
type ProjectColumn struct {
	ID        int64     `db:"id"         json:"id"`
	ProjectID int64     `db:"project_id" json:"project_id"`
	Name      string    `db:"name"       json:"name"`
	Position  int       `db:"position"   json:"position"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

// ProjectCard is a card in a column. IssueID and PullID are mutually exclusive,
// and a card with neither needs a Title (DB CHECK). Note holds the description.
// IssueTitle, IssueState, PullTitle, and PullState are populated via JOIN when
// listing cards.
type ProjectCard struct {
	ID        int64      `db:"id"          json:"id"`
	ColumnID  int64      `db:"column_id"   json:"column_id"`
	IssueID   *int64     `db:"issue_id"    json:"issue_id,omitempty"`
	PullID    *int64     `db:"pull_id"     json:"pull_id,omitempty"`
	Title     string     `db:"title"       json:"title"`
	Note      string     `db:"note"        json:"note"`
	DueDate   *time.Time `db:"due_date"    json:"due_date,omitempty"`
	Position  int        `db:"position"    json:"position"`
	CreatedAt time.Time  `db:"created_at"  json:"created_at"`

	// Populated via JOIN — not stored columns
	IssueTitle  string `db:"issue_title"  json:"issue_title,omitempty"`
	IssueNumber int    `db:"issue_number" json:"issue_number,omitempty"`
	IssueState  string `db:"issue_state"  json:"issue_state,omitempty"`
	PullTitle   string `db:"pull_title"   json:"pull_title,omitempty"`
	PullNumber  int    `db:"pull_number"  json:"pull_number,omitempty"`
	PullState   string `db:"pull_state"   json:"pull_state,omitempty"`

	// IssueHidden marks a card whose private issue the viewer may not see; its
	// IssueTitle, IssueNumber and IssueState are then left empty.
	IssueHidden bool `db:"-" json:"issue_hidden,omitempty"`
}

type CardUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// CardDetails is the full editable state of a card; SetCardDetails replaces all of it.
type CardDetails struct {
	Title, Description string
	DueDate            *time.Time
	IssueID, PullID    *int64
	AssigneeIDs        []int64
	LabelIDs           []int64
}

// CardTarget is an issue or pull request a board card can link to.
type CardTarget struct {
	ID     int64  `json:"id"`
	Kind   string `json:"kind"` // "issue" | "pull"
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
}
