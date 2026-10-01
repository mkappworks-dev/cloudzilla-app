package model

import "time"

// SignupToken is an emailed link that lets the mailbox owner finish creating an account.
type SignupToken struct {
	ID        int64     `db:"id"         json:"id"`
	Email     string    `db:"email"      json:"email"`
	ExpiresAt time.Time `db:"expires_at" json:"expires_at"`
}
