package view

import "github.com/mkappworks/cloudzilla/internal/model"

type LoginData struct {
	BasePage
	Error       string
	LDAPEnabled bool
	SAMLEnabled bool
}

type SetupData struct {
	BasePage
	Error string
}

type InviteData struct {
	BasePage
	Invitation *model.Invitation
	Error      string
}

// SecurityPageData is the view model for GET /settings/security.
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
type TOTPVerifyPageData struct {
	BasePage
	Error string
}
