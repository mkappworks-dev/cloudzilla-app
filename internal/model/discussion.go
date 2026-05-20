package model

import (
	"database/sql"
	"time"
)

type DiscussionCategory struct {
	ID          int64  `db:"id"          json:"id"`
	Name        string `db:"name"        json:"name"`
	Emoji       string `db:"emoji"       json:"emoji"`
	Description string `db:"description" json:"description"`
}

// Discussion represents a threaded discussion post in a repository.
type Discussion struct {
	ID         int64         `db:"id"          json:"id"`
	RepoID     int64         `db:"repo_id"      json:"repo_id"`
	CategoryID int64         `db:"category_id"  json:"category_id"`
	Number     int           `db:"number"       json:"number"`
	Title      string        `db:"title"        json:"title"`
	Body       string        `db:"body"         json:"body"`
	AuthorID   int64         `db:"author_id"    json:"author_id"`
	AuthorName string        `db:"author_name"  json:"author_name"`
	IsLocked   bool          `db:"is_locked"    json:"is_locked"`
	IsAnswered bool          `db:"is_answered"  json:"is_answered"`
	AnswerID   sql.NullInt64 `db:"answer_id"    json:"answer_id"`
	CreatedAt  time.Time     `db:"created_at"   json:"created_at"`
	UpdatedAt  time.Time     `db:"updated_at"   json:"updated_at"`
}

// DiscussionReply represents a reply within a discussion thread.
type DiscussionReply struct {
	ID           int64         `db:"id"            json:"id"`
	DiscussionID int64         `db:"discussion_id" json:"discussion_id"`
	ParentID     sql.NullInt64 `db:"parent_id"     json:"parent_id"`
	AuthorID     int64         `db:"author_id"     json:"author_id"`
	AuthorName   string        `db:"author_name"   json:"author_name"`
	Body         string        `db:"body"          json:"body"`
	IsAnswer     bool          `db:"is_answer"     json:"is_answer"`
	CreatedAt    time.Time     `db:"created_at"    json:"created_at"`
	UpdatedAt    time.Time     `db:"updated_at"    json:"updated_at"`
}
