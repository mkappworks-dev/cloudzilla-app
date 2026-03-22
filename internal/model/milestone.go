package model

import "time"

type Milestone struct {
	ID          int64      `db:"id"          json:"id"`
	RepoID      int64      `db:"repo_id"     json:"repo_id"`
	Number      int        `db:"number"      json:"number"`
	Title       string     `db:"title"       json:"title"`
	Description string     `db:"description" json:"description"`
	State       string     `db:"state"       json:"state"`
	DueDate     *time.Time `db:"due_date"    json:"due_date,omitempty"`
	ClosedAt    *time.Time `db:"closed_at"   json:"closed_at,omitempty"`
	CreatedAt   time.Time  `db:"created_at"  json:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at"  json:"updated_at"`
	OpenCount   int        `db:"-"           json:"open_count"`
	ClosedCount int        `db:"-"           json:"closed_count"`
}
