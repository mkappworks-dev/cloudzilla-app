package model

// PasswordResetState is what a password reset link resolves to.
type PasswordResetState string

const (
	// PasswordResetInvalid also covers used and replaced links, and links a
	// password change, sign-out-everywhere or email change has revoked.
	PasswordResetInvalid PasswordResetState = "invalid"
	PasswordResetExpired PasswordResetState = "expired"
	PasswordResetPending PasswordResetState = "pending"
	PasswordResetDone    PasswordResetState = "done"
)

// Who issued a password reset link.
const (
	PasswordResetByEmail = "email"
	PasswordResetByAdmin = "admin"
	PasswordResetByCLI   = "cli"
)

// PasswordResetLink is the account a link would reset. Everything but State is
// set only while the link is pending.
type PasswordResetLink struct {
	State       PasswordResetState
	UserID      int64
	Username    string
	Email       string
	IssuedBy    string
	TOTPEnabled bool
}
