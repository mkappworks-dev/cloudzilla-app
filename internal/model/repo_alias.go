package model

import "time"

// RepoAlias is a path a repository used to have; it redirects to RepoID while
// that repository is live.
type RepoAlias struct {
	ID        int64     `db:"id"         json:"id"`
	RepoID    int64     `db:"repo_id"    json:"repo_id"`
	OwnerName string    `db:"owner_name" json:"owner"`
	Name      string    `db:"name"       json:"name"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}
