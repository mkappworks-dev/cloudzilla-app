package view

import "github.com/mkappworks-dev/cloudzilla-app/internal/model"

// LoginData holds template data for the login page.
type LoginData struct {
	BasePage
	Error       string
	LDAPEnabled bool
	SAMLEnabled bool
}

// SetupData holds template data for the first-run setup wizard page.
type SetupData struct {
	BasePage
	Error string
}

// InviteData holds template data for the invitation acceptance page.
type InviteData struct {
	BasePage
	Invitation *model.Invitation
	Error      string
}

// SecurityPageData is the view model for GET /settings/security.
// SecurityPageData holds template data for the user security settings page.
type SecurityPageData struct {
	BasePage
	TOTPEnabled bool
	TOTPSecret  string   // pending secret, shown only before first verification
	OTPAuthURL  string   // otpauth:// URL for QR code (shown only when setting up)
	BackupCodes []string // raw backup codes, shown only once after enable
	Error       string
	Success     string
}

// TOTPVerifyPageData is the view model for GET /auth/2fa.
// TOTPVerifyPageData holds template data for the TOTP verification page.
type TOTPVerifyPageData struct {
	BasePage
	Error string
}
