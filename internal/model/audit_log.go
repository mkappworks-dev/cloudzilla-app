package model

import "time"

// AuditEntry represents a single row in the audit_log table.
// AuditEntry represents a single recorded action in the audit log.
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
// AuditFilter defines query parameters for filtering audit log entries.
type AuditFilter struct {
	ActorID    *int64
	Action     string
	TargetType string
	TargetID   *int64
}

const (
	AuditTargetOrg  = "org"
	AuditTargetRepo = "repo"
	AuditTargetUser = "user"
)

// Common action constants.
const (
	AuditActionLogin               = "login"
	AuditActionOAuthConnect        = "user.oauth.connect"
	AuditActionOAuthDisconnect     = "user.oauth.disconnect"
	AuditActionRepoCreate          = "repo.create"
	AuditActionRepoImport          = "repo.import"
	AuditActionRepoMirrorCreate    = "repo.mirror.create"
	AuditActionRepoMirrorUpdate    = "repo.mirror.update"
	AuditActionRepoMirrorDelete    = "repo.mirror.delete"
	AuditActionRepoDelete          = "repo.delete"
	AuditActionRepoTransfer        = "repo.transfer"
	AuditActionRepoTransferRequest = "repo.transfer.request"
	AuditActionRepoTransferCancel  = "repo.transfer.cancel"
	AuditActionRepoTransferDecline = "repo.transfer.decline"
	AuditActionOrgProfileUpdate    = "org.profile.update"
	AuditActionOrgDefaultsUpdate   = "org.defaults.update"
	AuditActionOrgDelete           = "org.delete"
	AuditActionOrgAvatarUpdate     = "org.avatar.update"
	AuditActionOrgAvatarRemove     = "org.avatar.remove"
	AuditActionEmailVerify         = "user.email.verify"
	AuditActionEmailChange         = "user.email.change"
	AuditActionSessionsRevoke      = "user.sessions.revoke"
	AuditActionPasswordChange      = "user.password.change"
	AuditActionPasswordReset       = "user.password.reset"
	AuditActionPasswordResetLink   = "user.password.reset_link"

	AuditActionAdminUserSuspend           = "admin.user.suspend"
	AuditActionAdminUserUnsuspend         = "admin.user.unsuspend"
	AuditActionAdminUserPromote           = "admin.user.promote"
	AuditActionAdminUserDemote            = "admin.user.demote"
	AuditActionAdminUser2FAReset          = "admin.user.2fa_reset"
	AuditActionAdminUserCredentialsRevoke = "admin.user.credentials_revoke"
	AuditActionAdminUserDelete            = "admin.user.delete"
)
