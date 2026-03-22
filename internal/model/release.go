package model

import "time"

type Release struct {
	ID           int64      `db:"id"            json:"id"`
	RepoID       int64      `db:"repo_id"       json:"repo_id"`
	TagName      string     `db:"tag_name"      json:"tag_name"`
	Name         string     `db:"name"          json:"name"`
	Body         string     `db:"body"          json:"body"`
	IsPrerelease bool       `db:"is_prerelease" json:"is_prerelease"`
	IsDraft      bool       `db:"is_draft"      json:"is_draft"`
	AuthorID     int64      `db:"author_id"     json:"author_id"`
	CreatedAt    time.Time  `db:"created_at"    json:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at"    json:"updated_at"`
	PublishedAt  *time.Time `db:"published_at"  json:"published_at,omitempty"`
}
