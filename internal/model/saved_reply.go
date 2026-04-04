package model

import "time"

type SavedReply struct {
	ID        int64     `db:"id"         json:"id"`
	UserID    int64     `db:"user_id"    json:"user_id"`
	Title     string    `db:"title"      json:"title"`
	Body      string    `db:"body"       json:"body"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}
