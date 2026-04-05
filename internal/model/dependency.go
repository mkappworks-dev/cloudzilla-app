package model

import "time"

// RepoDependency is a single dependency entry for a repository.
type RepoDependency struct {
	ID         int64     `db:"id"          json:"id"`
	RepoID     int64     `db:"repo_id"     json:"repo_id"`
	PackageMgr string    `db:"package_mgr" json:"package_mgr"`
	Package    string    `db:"package"     json:"package"`
	Version    string    `db:"version"     json:"version"`
	IsDev      bool      `db:"is_dev"      json:"is_dev"`
	UpdatedAt  time.Time `db:"updated_at"  json:"updated_at"`
}
