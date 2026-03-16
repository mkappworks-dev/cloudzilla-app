package model

import "time"

type SSHKey struct {
	ID          int64     `db:"id"          json:"id"`
	UserID      int64     `db:"user_id"     json:"user_id"`
	Title       string    `db:"title"       json:"title"`
	PublicKey   string    `db:"public_key"  json:"public_key"`
	Fingerprint string    `db:"fingerprint" json:"fingerprint"`
	CreatedAt   time.Time `db:"created_at"  json:"created_at"`
}
