package model

import "time"

const (
	WatchLevelWatching     = "watching"
	WatchLevelReleasesOnly = "releases_only"
	WatchLevelIgnoring     = "ignoring"
)

// Watch represents a user's watch subscription to a repository.
type Watch struct {
	ID        int64     `db:"id"         json:"id"`
	UserID    int64     `db:"user_id"    json:"user_id"`
	RepoID    int64     `db:"repo_id"    json:"repo_id"`
	Level     string    `db:"level"      json:"level"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}
