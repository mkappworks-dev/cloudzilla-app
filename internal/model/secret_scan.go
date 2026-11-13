package model

import "time"

const (
	SecretAlertOpen   = "open"
	SecretAlertClosed = "closed"

	SecretResolutionRevoked       = "revoked"
	SecretResolutionFalsePositive = "false_positive"
	SecretResolutionUsedInTests   = "used_in_tests"

	SecretEventFound       = "found"
	SecretEventBlocked     = "blocked"
	SecretEventClosed      = "closed"
	SecretEventReopened    = "reopened"
	SecretEventAllowed     = "allowed"
	SecretEventAutoRevoked = "auto_revoked"

	SecretTypeCloudzillaToken = "cloudzilla_token"
)

type SecretScanAlert struct {
	ID          int64      `db:"id"`
	RepoID      int64      `db:"repo_id"`
	Number      int        `db:"number"`
	SecretType  string     `db:"secret_type"`
	SecretHash  string     `db:"secret_hash"`
	SecretHint  string     `db:"secret_hint"`
	State       string     `db:"state"`
	Resolution  string     `db:"resolution"`
	ResolvedBy  *int64     `db:"resolved_by"`
	ResolvedAt  *time.Time `db:"resolved_at"`
	AutoRevoked bool       `db:"auto_revoked"`
	Incomplete  bool       `db:"incomplete"`
	CreatedAt   time.Time  `db:"created_at"`

	// Blocked is derived: no location is on a commit a ref reaches yet.
	Blocked bool `db:"-"`
}

type SecretScanLocation struct {
	ID         int64  `db:"id"`
	AlertID    int64  `db:"alert_id"`
	CommitSHA  string `db:"commit_sha"`
	FilePath   string `db:"file_path"`
	LineNumber int    `db:"line_number"`
	ReachedRef bool   `db:"reached_ref"`
}

type SecretScanEvent struct {
	ID        int64     `db:"id"`
	AlertID   int64     `db:"alert_id"`
	Kind      string    `db:"kind"`
	ActorID   *int64    `db:"actor_id"`
	Ref       string    `db:"ref"`
	CommitSHA string    `db:"commit_sha"`
	Detail    string    `db:"detail"`
	CreatedAt time.Time `db:"created_at"`
}

// SecretSighting is where one scan saw a secret. ActorID 0 means no user.
type SecretSighting struct {
	CommitSHA  string
	FilePath   string
	Line       int
	Ref        string
	ActorID    int64
	Incomplete bool
	Detail     string
}
