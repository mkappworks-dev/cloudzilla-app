package view

import (
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

// SettingsData holds template data for the user account settings page.
type SettingsData struct {
	BasePage
	User                model.User
	SSHKeys             []model.SSHKey
	Tokens              []model.AccessToken
	SavedReplies        []model.SavedReply
	OAuthApps           []model.OAuthApp
	OAuthAuthorizations []model.OAuthAuthorization
	TOTPEnabled         bool
	TOTPPendingSecret   string
	TOTPQRCode          string
	BackupCodes         []string
	NewToken            string
	NoreplyEmail        string
	// QuotaSummary is "" when the instance sets no quota for the user.
	QuotaSummary string
	ProfileSaved bool
	ProfileError string
	// EmailVerificationAvailable is false when no SMTP server is configured.
	EmailVerificationAvailable bool
	// VerificationLinkSent is whether a live link for the current address is out.
	VerificationLinkSent bool
	SessionsRevoked      bool
	PasswordChanged      bool
	PasswordError        string
	Confirm              components.ConfirmFactors
	GoogleConfigured     bool
	GoogleConnected      bool
	HasPassword          bool
	// Codes from the Google connect flow, shown beside the control rather than atop the page.
	ConnectedAccountsError  string
	ConnectedAccountsNotice string
}

// OAuthAppsFragData holds template data for the OAuth apps list HTMX fragment.
type OAuthAppsFragData struct {
	Apps []model.OAuthApp
	// Set only when rendering the create response: the store keeps a bcrypt hash, so this is the one chance to show it.
	NewClientSecret string
	NewClientID     string
}

// OAuthAuthorizationsFragData holds template data for the authorized OAuth apps HTMX fragment.
type OAuthAuthorizationsFragData struct {
	Authorizations []model.OAuthAuthorization
}

// NotificationsData holds template data for the notifications page.
type NotificationsData struct {
	BasePage
	Notifications []model.Notification

	Filter     string // "inbox", "unread" or "read"
	Page       int
	TotalPages int
	PerPage    int
	Total      int // matching Filter

	InboxCount  int
	UnreadCount int
	ReadCount   int
}

// AdminSettingsData holds template data for the admin settings page.
type AdminSettingsData struct {
	BasePage
	Settings    []model.SiteSetting
	Invitations []model.Invitation
	Confirm     components.ConfirmFactors
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
	Confirm    components.ConfirmFactors
}

// OAuthAuthorizeData holds template data for the OAuth authorization consent page.
type OAuthAuthorizeData struct {
	BasePage
	App         model.OAuthApp
	Scopes      []string
	RedirectURI string
	State       string
	Confirm     components.ConfirmFactors
	Error       string
}
