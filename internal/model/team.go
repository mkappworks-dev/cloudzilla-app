package model

import "time"

type Team struct {
	ID          int64     `db:"id"          json:"id"`
	OrgID       int64     `db:"org_id"      json:"org_id"`
	Name        string    `db:"name"        json:"name"`
	Slug        string    `db:"slug"        json:"slug"`
	Description string    `db:"description" json:"description"`
	MemberCount int       `db:"-"           json:"member_count"`
	CreatedAt   time.Time `db:"created_at"  json:"created_at"`
}

type TeamMember struct {
	UserID   int64  `db:"user_id"  json:"user_id"`
	Username string `db:"username" json:"username"`
}
