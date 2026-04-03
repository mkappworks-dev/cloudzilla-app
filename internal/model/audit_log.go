package model

import "time"

// AuditEntry represents a single row in the audit_log table.
type AuditEntry struct {
	ID         int64          `db:"id"          json:"id"`
	ActorID    *int64         `db:"actor_id"    json:"actor_id"`
	ActorName  string         `db:"actor_name"  json:"actor_name"`
	Action     string         `db:"action"      json:"action"`
	TargetType string         `db:"target_type" json:"target_type"`
	TargetID   *int64         `db:"target_id"   json:"target_id"`
	TargetName string         `db:"target_name" json:"target_name"`
	IPAddress  string         `db:"ip_address"  json:"ip_address"`
	UserAgent  string         `db:"user_agent"  json:"user_agent"`
	Metadata   map[string]any `db:"-"           json:"metadata"`
	CreatedAt  time.Time      `db:"created_at"  json:"created_at"`
}

// AuditFilter restricts which audit entries are returned by List.
type AuditFilter struct {
	ActorID    *int64
	Action     string
	TargetType string
}

// Common action constants.
const (
	AuditActionLogin        = "login"
	AuditActionRepoCreate   = "repo.create"
	AuditActionRepoDelete   = "repo.delete"
	AuditActionRepoTransfer = "repo.transfer"
)
