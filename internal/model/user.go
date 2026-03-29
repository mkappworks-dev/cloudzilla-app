package model

import (
	"database/sql"
	"time"
)

type User struct {
	ID              int64          `db:"id"               json:"id"`
	Username        string         `db:"username"         json:"username"`
	Email           string         `db:"email"            json:"email"`
	PasswordHash    string         `db:"password_hash"    json:"-"`
	Bio             string         `db:"bio"              json:"bio"`
	AvatarURL       string         `db:"avatar_url"       json:"avatar_url"`
	OAuthProvider   string         `db:"oauth_provider"   json:"-"`
	OAuthID         string         `db:"oauth_id"         json:"-"`
	IsSuperadmin    bool           `db:"is_superadmin"    json:"-"`
	IsInvited       bool           `db:"is_invited"       json:"-"`
	TOTPSecret      sql.NullString `db:"totp_secret"      json:"-"`
	TOTPEnabled     bool           `db:"totp_enabled"     json:"-"`
	TOTPBackupCodes []string       `db:"-"                json:"-"`
	CreatedAt       time.Time      `db:"created_at"       json:"created_at"`
	UpdatedAt       time.Time      `db:"updated_at"       json:"updated_at"`
}
