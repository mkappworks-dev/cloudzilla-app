package model

import "time"

type DeployKey struct {
	ID          int64      `db:"id"           json:"id"`
	RepoID      int64      `db:"repo_id"      json:"repo_id"`
	Title       string     `db:"title"        json:"title"`
	Fingerprint string     `db:"fingerprint"  json:"fingerprint"`
	PublicKey   string     `db:"public_key"   json:"public_key"`
	ReadOnly    bool       `db:"read_only"    json:"read_only"`
	LastUsedAt  *time.Time `db:"last_used_at" json:"last_used_at"`
	CreatedAt   time.Time  `db:"created_at"   json:"created_at"`
}
