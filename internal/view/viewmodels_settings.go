package view

import "github.com/mkappworks/cloudzilla/internal/model"

type SettingsData struct {
	BasePage
	SSHKeys []model.SSHKey
}

type NotificationsData struct {
	BasePage
	Notifications []model.Notification
	UnreadCount   int
}

type NotificationsFragData struct {
	Notifications []model.Notification
}

type NotificationItemFragData struct {
	Notification model.Notification
}

type AdminSettingsData struct {
	BasePage
	Settings    []model.SiteSetting
	Invitations []model.Invitation
}

type AdminSettingsFragData struct {
	Settings []model.SiteSetting
}

type AdminInvitationsFragData struct {
	Invitations []model.Invitation
}

// SSHKeysFragData is used by the SSH keys fragment.
type SSHKeysFragData struct {
	SSHKeys []model.SSHKey
}

// Tokens page
type TokensData struct {
	BasePage
	Tokens   []model.AccessToken
	NewToken string // raw token, shown only once after creation
}

// Tokens list fragment
type TokensListFragData struct {
	Tokens []model.AccessToken
}

// AuditLogData holds data for the admin audit log page.
type AuditLogData struct {
	BasePage
	Entries    []model.AuditEntry
	Filter     model.AuditFilter
	TotalCount int
	Page       int
	PerPage    int
}

// SSOSettingsData is the view model for GET/POST /admin/sso.
type SSOSettingsData struct {
	BasePage
	LDAPConfig *model.SSOConfig
	SAMLConfig *model.SSOConfig
	Error      string
	Success    string
}

// Notification settings page
type NotificationSettingsData struct {
	BasePage
	EmailNotifications bool
	EmailDigest        string
}

// OAuth Apps pages
type OAuthAuthorizeData struct {
	BasePage
	App         model.OAuthApp
	Scopes      []string
	RedirectURI string
	State       string
}

type OAuthAppsData struct {
	BasePage
	Apps           []model.OAuthApp
	Authorizations []model.OAuthAuthorization
}
