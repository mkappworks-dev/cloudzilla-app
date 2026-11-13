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
	// Sort is one of AuditSort's keys; "" orders by time, newest first.
	Sort string
	Desc bool
}

// AuditSort lists the columns /admin/audit-log sorts by.
var AuditSort = SortSpec{{Key: "time", DescFirst: true}, {Key: "actor"}, {Key: "action"}, {Key: "target"}}

const (
	AuditTargetOrg  = "org"
	AuditTargetRepo = "repo"
	AuditTargetPull = "pull"
	AuditTargetUser = "user"
	// AuditTargetInstanceSigningKey names the key by fingerprint.
	AuditTargetInstanceSigningKey = "instance_signing_key"
	// AuditTargetSecretAlert names the alert by "<owner>/<repo>#<number>".
	AuditTargetSecretAlert = "secret_scan_alert"
)

// Common action constants.
const (
	AuditActionLogin                = "login"
	AuditActionOAuthConnect         = "user.oauth.connect"
	AuditActionOAuthDisconnect      = "user.oauth.disconnect"
	AuditActionRepoCreate           = "repo.create"
	AuditActionRepoImport           = "repo.import"
	AuditActionRepoMirrorCreate     = "repo.mirror.create"
	AuditActionRepoMirrorUpdate     = "repo.mirror.update"
	AuditActionRepoMirrorDelete     = "repo.mirror.delete"
	AuditActionRepoDelete           = "repo.delete"
	AuditActionRepoRename           = "repo.rename"
	AuditActionRepoTransfer         = "repo.transfer"
	AuditActionRepoTransferRequest  = "repo.transfer.request"
	AuditActionRepoTransferCancel   = "repo.transfer.cancel"
	AuditActionRepoTransferDecline  = "repo.transfer.decline"
	AuditActionPullReviewDismiss    = "pull.review.dismiss"
	AuditActionOrgProfileUpdate     = "org.profile.update"
	AuditActionOrgDefaultsUpdate    = "org.defaults.update"
	AuditActionOrgDelete            = "org.delete"
	AuditActionOrgRename            = "org.rename"
	AuditActionOrgAvatarUpdate      = "org.avatar.update"
	AuditActionOrgAvatarRemove      = "org.avatar.remove"
	AuditActionTeamCreate           = "org.team.create"
	AuditActionTeamUpdate           = "org.team.update"
	AuditActionTeamDelete           = "org.team.delete"
	AuditActionTeamMemberAdd        = "org.team.member_add"
	AuditActionTeamMemberRemove     = "org.team.member_remove"
	AuditActionTeamRepoGrant        = "org.team.repo_grant"
	AuditActionTeamRepoRevoke       = "org.team.repo_revoke"
	AuditActionEmailVerify          = "user.email.verify"
	AuditActionEmailChange          = "user.email.change"
	AuditActionEmailAdd             = "user.email.add"
	AuditActionEmailRemove          = "user.email.remove"
	AuditActionExportRequest        = "user.export.request"
	AuditActionExportDownload       = "user.export.download"
	AuditActionSessionsRevoke       = "user.sessions.revoke"
	AuditActionPasswordChange       = "user.password.change"
	AuditActionPasswordReset        = "user.password.reset"
	AuditActionPasswordResetLink    = "user.password.reset_link"
	AuditActionDeviceApprove        = "user.device.approve"
	AuditActionDeviceDeny           = "user.device.deny"
	AuditActionTokenCreate          = "user.token.create"
	AuditActionGPGKeyAdd            = "user.gpg_key.add"
	AuditActionGPGKeyDelete         = "user.gpg_key.delete"
	AuditActionGPGKeyUpdate         = "user.gpg_key.update"
	AuditActionGPGKeyRevoke         = "user.gpg_key.revoke"
	AuditActionGPGKeyRestore        = "user.gpg_key.restore"
	AuditActionSSHSigningKeyAdd     = "user.ssh_signing_key.add"
	AuditActionSSHSigningKeyDelete  = "user.ssh_signing_key.delete"
	AuditActionSSHSigningKeyRevoke  = "user.ssh_signing_key.revoke"
	AuditActionSSHSigningKeyRestore = "user.ssh_signing_key.restore"

	AuditActionAdminUserSuspend           = "admin.user.suspend"
	AuditActionAdminUserUnsuspend         = "admin.user.unsuspend"
	AuditActionAdminUserPromote           = "admin.user.promote"
	AuditActionAdminUserDemote            = "admin.user.demote"
	AuditActionAdminUser2FAReset          = "admin.user.2fa_reset"
	AuditActionAdminUserCredentialsRevoke = "admin.user.credentials_revoke"
	AuditActionAdminOrgOwnerAdd           = "admin.org.owner_add"
	AuditActionAdminUserDelete            = "admin.user.delete"
	AuditActionAdminSigningKeyRotate      = "admin.signing_key.rotate"
)

// Secret scanning. Metadata carries the repo and secret type, never the secret or its hint.
const (
	AuditActionSecretScanClose  = "secret_scan.close"
	AuditActionSecretScanReopen = "secret_scan.reopen"
	AuditActionSecretScanAllow  = "secret_scan.allow"
	AuditActionTokenAutoRevoke  = "token.auto_revoke"
)

// Admin mode: entering and leaving, and each mutating request that only passed through it.
const (
	AuditActionAdminModeEnter  = "admin.mode.enter"
	AuditActionAdminModeLeave  = "admin.mode.leave"
	AuditActionAdminModeAction = "admin.mode.action"
)
