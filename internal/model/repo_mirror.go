package model

import "time"

// RepoMirror is a pull mirror's upstream, credentials and sync state.
type RepoMirror struct {
	RepoID       int64
	RemoteURL    string
	AuthUsername string
	// AuthTokenEnc is the token sealed by secretbox; nil when there is none.
	AuthTokenEnc        []byte
	Interval            time.Duration
	NextSyncAt          time.Time
	LeaseUntil          *time.Time
	LastSyncAt          *time.Time
	LastSuccessAt       *time.Time
	LastError           string
	ConsecutiveFailures int
	CreatedBy           int64
	CreatedAt           time.Time
	UpdatedAt           time.Time

	// Set by lookups that join the repo, so a sync can find it on disk.
	OwnerName string
	RepoName  string
}
