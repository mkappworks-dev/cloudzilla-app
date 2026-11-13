package model

import "time"

// GPGKey is an armored OpenPGP public key registered to sign commits.
type GPGKey struct {
	ID          int64       `db:"id"          json:"id"`
	UserID      int64       `db:"user_id"     json:"user_id"`
	KeyID       string      `db:"key_id"      json:"key_id"`
	Fingerprint string      `db:"fingerprint" json:"fingerprint"`
	PublicKey   string      `db:"public_key"  json:"public_key"`
	ExpiresAt   *time.Time  `db:"expires_at"  json:"expires_at"`
	RevokedAt   *time.Time  `db:"revoked_at"  json:"revoked_at"`
	RemovedAt   *time.Time  `db:"removed_at"  json:"removed_at"`
	CreatedAt   time.Time   `db:"created_at"  json:"created_at"`
	Status      string      `db:"-"           json:"status"`
	Subkeys     []GPGSubkey `db:"-"           json:"subkeys"`
	Emails      []string    `db:"-"           json:"emails"`
}

// GPGSubkey is a subkey of a GPGKey; only those with CanSign verify commits.
type GPGSubkey struct {
	ID          int64      `db:"id"          json:"id"`
	GPGKeyID    int64      `db:"gpg_key_id"  json:"-"`
	KeyID       string     `db:"key_id"      json:"key_id"`
	Fingerprint string     `db:"fingerprint" json:"fingerprint"`
	CanSign     bool       `db:"can_sign"    json:"can_sign"`
	ExpiresAt   *time.Time `db:"expires_at"  json:"expires_at"`
	RevokedAt   *time.Time `db:"revoked_at"  json:"revoked_at"`
	RemovedAt   *time.Time `db:"removed_at"  json:"removed_at"`
}

// SSHSigningKey is an SSH public key registered to sign commits, separate from the auth keys in ssh_keys.
type SSHSigningKey struct {
	ID          int64      `db:"id"          json:"id"`
	UserID      int64      `db:"user_id"     json:"user_id"`
	Title       string     `db:"title"       json:"title"`
	PublicKey   string     `db:"public_key"  json:"public_key"`
	Fingerprint string     `db:"fingerprint" json:"fingerprint"`
	RemovedAt   *time.Time `db:"removed_at"  json:"removed_at"`
	RevokedAt   *time.Time `db:"revoked_at"  json:"revoked_at"`
	CreatedAt   time.Time  `db:"created_at"  json:"created_at"`
	Status      string     `db:"-"           json:"status"`
}

const (
	SigningKeyActive  = "active"
	SigningKeyRemoved = "removed"
	SigningKeyRevoked = "revoked"
)

// SigningKeyStatus is revoked over removed over active: revocation is final and shows whatever else happened.
func SigningKeyStatus(removedAt, revokedAt *time.Time) string {
	switch {
	case revokedAt != nil:
		return SigningKeyRevoked
	case removedAt != nil:
		return SigningKeyRemoved
	}
	return SigningKeyActive
}
