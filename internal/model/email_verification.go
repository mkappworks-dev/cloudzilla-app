package model

// EmailVerificationState is what an email verification link resolves to.
type EmailVerificationState string

const (
	// EmailVerificationInvalid also covers used links and links for an address
	// the account no longer has, so a link never reveals more than that it failed.
	EmailVerificationInvalid  EmailVerificationState = "invalid"
	EmailVerificationExpired  EmailVerificationState = "expired"
	EmailVerificationPending  EmailVerificationState = "pending"
	EmailVerificationVerified EmailVerificationState = "verified"
)

// EmailVerificationLink is what a link would verify. Username and Email are
// set only while the link is pending: the person holding it was sent it, and
// seeing whose account it is lets them refuse to vouch for someone else's.
type EmailVerificationLink struct {
	State    EmailVerificationState
	Username string
	Email    string
}
