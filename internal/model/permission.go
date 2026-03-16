package model

import "time"

type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleWriter Role = "writer"
	RoleReader Role = "reader"
)

type Permission struct {
	ID        int64     `db:"id"         json:"id"`
	UserID    int64     `db:"user_id"    json:"user_id"`
	RepoID    int64     `db:"repo_id"    json:"repo_id"`
	Role      Role      `db:"role"       json:"role"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}
