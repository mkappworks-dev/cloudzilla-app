package view

import "github.com/mkappworks-dev/cloudzilla-app/internal/model"

// LoginData holds template data for the login page.
type LoginData struct {
	BasePage
	Error             string
	LDAPEnabled       bool
	SAMLEnabled       bool
	AllowRegistration bool
}

// RegisterData holds template data for the public registration page.
type RegisterData struct {
	BasePage
	Error    string
	Username string
	Email    string
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

// TOTPVerifyPageData is the view model for GET /auth/2fa.
// TOTPVerifyPageData holds template data for the TOTP verification page.
type TOTPVerifyPageData struct {
	BasePage
	Error string
}
