package model

import "time"

// Gist represents a shareable snippet of code or text with multiple files.
type Gist struct {
	ID           string    `db:"id"             json:"id"`
	OwnerID      int64     `db:"owner_id"       json:"owner_id"`
	OwnerName    string    `db:"owner_name"     json:"owner_name"`
	Description  string    `db:"description"    json:"description"`
	Public       bool      `db:"public"         json:"public"`
	ForkedFromID *string   `db:"forked_from_id" json:"forked_from_id,omitempty"`
	CreatedAt    time.Time `db:"created_at"     json:"created_at"`
	UpdatedAt    time.Time `db:"updated_at"     json:"updated_at"`
}

// GistListRow extends Gist with aggregate counts for list views.
type GistListRow struct {
	Gist
	FileCount int64 `db:"file_count" json:"file_count"`
	StarCount int64 `db:"star_count" json:"star_count"`
	ForkCount int64 `db:"fork_count" json:"fork_count"`
}

// GistFile represents a single file within a gist.
type GistFile struct {
	ID       int64  `db:"id"       json:"id"`
	GistID   string `db:"gist_id"  json:"gist_id"`
	Filename string `db:"filename" json:"filename"`
	Content  string `db:"content"  json:"content"`
}
