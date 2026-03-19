package model

import "time"

type Repository struct {
	ID            int64     `db:"id"             json:"id"`
	OwnerID       int64     `db:"owner_id"       json:"owner_id"`
	OwnerName     string    `db:"owner_name"     json:"owner_name,omitempty"`
	OrgID         int64     `db:"org_id"         json:"org_id,omitempty"`
	Name          string    `db:"name"           json:"name"`
	Description   string    `db:"description"    json:"description"`
	Private       bool      `db:"private"        json:"private"`
	DefaultBranch string    `db:"default_branch" json:"default_branch"`
	CreatedAt     time.Time `db:"created_at"     json:"created_at"`
	UpdatedAt     time.Time `db:"updated_at"     json:"updated_at"`
	IsFork        bool      `db:"is_fork"        json:"is_fork"`
	ForkOfID      *int64    `db:"fork_of_id"     json:"fork_of_id,omitempty"`
	ForkOfOwner   string    `db:"-"              json:"fork_of_owner,omitempty"`
	ForkOfName    string    `db:"-"              json:"fork_of_name,omitempty"`
	ForkCount     int       `db:"fork_count"     json:"fork_count"`
}
