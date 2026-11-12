package model

import "time"

// UserEmail is an address besides User.Email. It credits commits once verified and does nothing else.
type UserEmail struct {
	ID         int64
	UserID     int64
	Email      string
	VerifiedAt *time.Time
	CreatedAt  time.Time
}

func (e UserEmail) Verified() bool {
	return e.VerifiedAt != nil
}
