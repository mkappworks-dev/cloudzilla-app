package model

import (
	"database/sql"
	"time"
)

const (
	DeviceGrantPending  = "pending"
	DeviceGrantApproved = "approved"
	DeviceGrantDenied   = "denied"
	DeviceGrantConsumed = "consumed"
)

// DeviceGrant is a pending `cz auth login`: the device code's hash, the short code the
// user types in the browser, and what the user decided.
type DeviceGrant struct {
	ID             int64
	DeviceCodeHash string
	UserCode       string
	Scopes         []string
	DeviceName     string
	Status         string
	UserID         sql.NullInt64
	RequesterIP    string
	IntervalSecs   int
	LastPolledAt   *time.Time
	ExpiresAt      time.Time
	CreatedAt      time.Time
}
