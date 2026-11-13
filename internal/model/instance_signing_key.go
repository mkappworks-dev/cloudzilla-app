package model

import "time"

// InstanceSigningKey is a key the server signs its own commits with. Retired keys stay so older commits verify.
type InstanceSigningKey struct {
	ID            int64      `db:"id"`
	PublicKey     string     `db:"public_key"`
	Fingerprint   string     `db:"fingerprint"`
	SealedPrivate []byte     `db:"sealed_private"`
	CreatedAt     time.Time  `db:"created_at"`
	RetiredAt     *time.Time `db:"retired_at"`
}
