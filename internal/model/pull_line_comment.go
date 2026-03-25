package model

import "time"

type PullLineComment struct {
	ID             int64     `db:"id"              json:"id"`
	PullID         int64     `db:"pull_id"         json:"pull_id"`
	RepoID         int64     `db:"repo_id"         json:"repo_id"`
	AuthorID       int64     `db:"author_id"       json:"author_id"`
	AuthorName     string    `db:"author_name"     json:"author_name"`
	Path           string    `db:"path"            json:"path"`
	DiffSide       string    `db:"diff_side"       json:"diff_side"`
	Line           int       `db:"line"            json:"line"`
	Body           string    `db:"body"            json:"body"`
	IsSuggestion   bool      `db:"is_suggestion"   json:"is_suggestion"`
	SuggestionBody string    `db:"suggestion_body" json:"suggestion_body"`
	CreatedAt      time.Time `db:"created_at"      json:"created_at"`
	UpdatedAt      time.Time `db:"updated_at"      json:"updated_at"`
}
