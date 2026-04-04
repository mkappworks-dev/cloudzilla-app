package model

import (
	"database/sql"
	"time"
)

type OAuthApp struct {
	ID              int64     `db:"id"            json:"id"`
	OwnerID         int64     `db:"owner_id"      json:"owner_id"`
	Name            string    `db:"name"          json:"name"`
	ClientID        string    `db:"client_id"     json:"client_id"`
	ClientSecret    string    `db:"client_secret" json:"-"`
	RedirectURIs    []string  `db:"-"             json:"redirect_uris"`
	RedirectURIsRaw string    `db:"redirect_uris" json:"-"`
	HomepageURL     string    `db:"homepage_url"  json:"homepage_url"`
	Description     string    `db:"description"   json:"description"`
	CreatedAt       time.Time `db:"created_at"    json:"created_at"`
}

type OAuthAuthorization struct {
	ID            int64          `db:"id"              json:"id"`
	AppID         int64          `db:"app_id"          json:"app_id"`
	UserID        int64          `db:"user_id"         json:"user_id"`
	Code          sql.NullString `db:"code"            json:"-"`
	TokenHash     sql.NullString `db:"token_hash"      json:"-"`
	Scopes        []string       `db:"-"               json:"scopes"`
	ScopesRaw     string         `db:"scopes"          json:"-"`
	CodeExpiresAt *time.Time     `db:"code_expires_at" json:"-"`
	CreatedAt     time.Time      `db:"created_at"      json:"created_at"`
}
