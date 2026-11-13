package model

import "time"

const (
	RunnerScopeInstance   = "instance"
	RunnerScopeRepository = "repository"
)

// CIRunner is a registered cz-runner. Its token is kept only as a hash.
type CIRunner struct {
	ID            int64      `db:"id"`
	Scope         string     `db:"scope"`
	RepoID        *int64     `db:"repo_id"`
	Name          string     `db:"name"`
	Labels        []string   `db:"labels"`
	Version       string     `db:"version"`
	LastContactAt *time.Time `db:"last_contact_at"`
	RevokedAt     *time.Time `db:"revoked_at"`
	CreatedBy     *int64     `db:"created_by"`
	CreatedAt     time.Time  `db:"created_at"`
}

// CIRegistrationToken is a one-time token that a runner trades for its own token.
type CIRegistrationToken struct {
	ID        int64      `db:"id"`
	Scope     string     `db:"scope"`
	RepoID    *int64     `db:"repo_id"`
	CreatedBy *int64     `db:"created_by"`
	CreatedAt time.Time  `db:"created_at"`
	ExpiresAt time.Time  `db:"expires_at"`
	UsedAt    *time.Time `db:"used_at"`
}
