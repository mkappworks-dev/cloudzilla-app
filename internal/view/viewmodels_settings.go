package view

import "github.com/mkappworks-dev/cloudzilla-app/internal/model"

// SettingsData holds template data for the consolidated user account settings page.
// All sections (profile, security, SSH keys, tokens, notifications, sessions, emails, danger)
// render from a single page handler.
type SettingsData struct {
	BasePage
	User              model.User
	SSHKeys           []model.SSHKey
	Tokens            []model.AccessToken
	TOTPEnabled       bool
	TOTPPendingSecret string
	TOTPOTPAuthURL    string
	BackupCodes       []string
	NewToken          string
	ProfileSaved      bool
	ProfileError      string
}

// NotificationsData holds template data for the notifications page.
type NotificationsData struct {
	BasePage
	Notifications []model.Notification
	UnreadCount   int
}

// NotificationsFragData holds template data for the notifications list HTMX fragment.
type NotificationsFragData struct {
	Notifications []model.Notification
}

// NotificationItemFragData holds template data for a single notification item fragment.
type NotificationItemFragData struct {
	Notification model.Notification
}

// AdminSettingsData holds template data for the admin settings page.
type AdminSettingsData struct {
	BasePage
	Settings    []model.SiteSetting
	Invitations []model.Invitation
}

// AdminSettingsFragData holds template data for the admin settings HTMX fragment.
type AdminSettingsFragData struct {
	Settings []model.SiteSetting
}

// AdminInvitationsFragData holds template data for the admin invitations HTMX fragment.
type AdminInvitationsFragData struct {
	Invitations []model.Invitation
}

// SSHKeysFragData is used by the SSH keys fragment.
// SSHKeysFragData holds template data for the SSH keys HTMX fragment.
type SSHKeysFragData struct {
	SSHKeys []model.SSHKey
}

// Tokens list fragment
// TokensListFragData holds template data for the PAT list HTMX fragment.
type TokensListFragData struct {
	Tokens []model.AccessToken
}

// AuditLogData holds data for the admin audit log page.
// AuditLogData holds template data for the audit log page.
type AuditLogData struct {
	BasePage
	Entries    []model.AuditEntry
	Filter     model.AuditFilter
	TotalCount int
	Page       int
	PerPage    int
}

// SSOSettingsData is the view model for GET/POST /admin/sso.
// SSOSettingsData holds template data for the SSO configuration page.
type SSOSettingsData struct {
	BasePage
	LDAPConfig *model.SSOConfig
	SAMLConfig *model.SSOConfig
	Error      string
	Success    string
}

// OAuthAuthorizeData holds template data for the OAuth authorization consent page.
type OAuthAuthorizeData struct {
	BasePage
	App         model.OAuthApp
	Scopes      []string
	RedirectURI string
	State       string
}
