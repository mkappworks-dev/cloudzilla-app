package view

import "github.com/mkappworks-dev/cloudzilla-app/internal/model"

// LoginData holds template data for the login page.
type LoginData struct {
	BasePage
	Error             string
	LDAPEnabled       bool
	SAMLEnabled       bool
	AllowRegistration bool
	Notice            string
	// Email is the address typed on the failed attempt.
	Email string
	// PasswordError sits on the password field; Error is for a refusal that is not about a field.
	PasswordError string
	// Next is the unvalidated return path; handlers run it through safeNextPath
	// before redirecting.
	Next string
}

// RegisterData holds template data for the public registration page.
type RegisterData struct {
	BasePage
	Username string
	Email    string
	// FieldErrors maps a field name (username, email, password) to its message.
	FieldErrors map[string]string
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
	Next  string
}

type VerifyEmailData struct {
	BasePage
	Link model.EmailVerificationLink
	// Token is set only while the link is pending, for the confirm form.
	Token string
}

// RegisterEmailData holds template data for the email-first registration form.
type RegisterEmailData struct {
	BasePage
	Email string
	Error string
}

// RegisterCheckInboxData holds template data for the page shown after every email-first registration submit.
type RegisterCheckInboxData struct {
	BasePage
}

// RegisterCompleteData holds template data for finishing a signup from its emailed link.
// Signup is nil when the link is unusable.
type RegisterCompleteData struct {
	BasePage
	Signup   *model.SignupToken
	Username string
	Error    string
}

// ForgotPasswordData holds template data for the forgot-password form. Sent
// replaces the form with the page every valid address gets.
type ForgotPasswordData struct {
	BasePage
	Available bool
	// Admins names who can issue a link when Available is false.
	Admins []string
	Sent   bool
	Email  string
	Error  string
}

// ResetPasswordData holds template data for choosing a new password from a reset link.
type ResetPasswordData struct {
	BasePage
	Link  model.PasswordResetLink
	Error string
}
