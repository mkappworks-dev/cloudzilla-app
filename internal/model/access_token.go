package model

import "time"

type AccessToken struct {
	ID         int64      `db:"id"           json:"id"`
	UserID     int64      `db:"user_id"      json:"user_id"`
	Name       string     `db:"name"         json:"name"`
	TokenHash  string     `db:"token_hash"   json:"-"`
	LastEight  string     `db:"last_eight"   json:"last_eight"`
	Scopes     []string   `db:"-"            json:"scopes"`
	ScopesRaw  string     `db:"scopes"       json:"-"`
	LastUsedAt *time.Time `db:"last_used_at" json:"last_used_at"`
	ExpiresAt  *time.Time `db:"expires_at"   json:"expires_at"`
	CreatedAt  time.Time  `db:"created_at"   json:"created_at"`
}
