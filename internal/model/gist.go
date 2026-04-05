package model

import "time"

// Gist represents a shareable snippet of code or text with multiple files.
type Gist struct {
	ID          string    `db:"id"          json:"id"`
	OwnerID     int64     `db:"owner_id"    json:"owner_id"`
	OwnerName   string    `db:"owner_name"  json:"owner_name"`
	Description string    `db:"description" json:"description"`
	Public      bool      `db:"public"      json:"public"`
	CreatedAt   time.Time `db:"created_at"  json:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"  json:"updated_at"`
}

// GistFile represents a single file within a gist.
type GistFile struct {
	ID       int64  `db:"id"       json:"id"`
	GistID   string `db:"gist_id"  json:"gist_id"`
	Filename string `db:"filename" json:"filename"`
	Content  string `db:"content"  json:"content"`
}
