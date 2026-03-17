package model

import "time"

type Invitation struct {
	ID          int64      `db:"id"            json:"id"`
	Token       string     `db:"token"         json:"token"`
	Email       string     `db:"email"         json:"email"`
	InvitedByID int64      `db:"invited_by_id" json:"invited_by_id"`
	ExpiresAt   time.Time  `db:"expires_at"    json:"expires_at"`
	AcceptedAt  *time.Time `db:"accepted_at"   json:"accepted_at"`
	CreatedAt   time.Time  `db:"created_at"    json:"created_at"`
}
