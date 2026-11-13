package model

import "time"

// CISecret is a repo secret. The sealed value stays in the store; no field here can hold the plaintext.
type CISecret struct {
	ID            int64
	RepoID        int64
	Name          string
	CreatedBy     *int64
	CreatedByName string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
