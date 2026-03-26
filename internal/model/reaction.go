package model

import "time"

// Reaction is a single emoji reaction row.
type Reaction struct {
	ID        int64     `db:"id"         json:"id"`
	UserID    int64     `db:"user_id"    json:"user_id"`
	CommentID int64     `db:"comment_id" json:"comment_id"`
	Emoji     string    `db:"emoji"      json:"emoji"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

// ReactionSummary aggregates reactions for one emoji on one comment.
type ReactionSummary struct {
	Emoji       string `db:"emoji"        json:"emoji"`
	Count       int    `db:"count"        json:"count"`
	UserReacted bool   `db:"user_reacted" json:"user_reacted"`
}
