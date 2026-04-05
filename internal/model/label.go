package model

import "time"

// Label represents a colored tag that can be applied to issues and pull requests.
type Label struct {
	ID          int64     `db:"id"          json:"id"`
	RepoID      int64     `db:"repo_id"     json:"repo_id"`
	Name        string    `db:"name"        json:"name"`
	Color       string    `db:"color"       json:"color"`
	Description string    `db:"description" json:"description"`
	CreatedAt   time.Time `db:"created_at"  json:"created_at"`
}
