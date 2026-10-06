package pages

import (
	"html/template"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/highlight"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

var emailDigestLabels = map[string]string{
	model.EmailDigestImmediate: "Immediately",
	model.EmailDigestDaily:     "Daily digest",
	model.EmailDigestWeekly:    "Weekly digest",
	model.EmailDigestNever:     "Never",
}

// SettingsErrorMessage is shared with handlers that answer HTMX requests with the message itself.
func SettingsErrorMessage(code string) string {
	switch code {
	case "invalid_email":
		return "Enter a valid email address."
	case "email_taken":
		return "That email is already in use."
	case "update_failed":
		return "Couldn't save your profile. Please try again."
	case "delete_confirm_mismatch":
		return "Confirmation username didn't match. Account was not deleted."
	case "delete_failed":
		return "Couldn't delete your account. Please try again."
	case "sole_org_owner":
		return "You are the only owner of an organization. Add another owner or delete the organization first. Account was not deleted."
	case "totp_setup_failed":
		return "Couldn't start two-factor setup. Please try again."
	case "totp_missing_fields":
		return "Secret and verification code are required."
	case "totp_missing_code":
		return "Verification code is required."
	case "totp_invalid_code":
		return "Invalid verification code. Please try again."
	case "reauth_failed":
		return "Your password or two-factor code was incorrect. Nothing was changed."
	case "reauth_throttled":
		return "Too many incorrect passwords or codes. Try again in 15 minutes."
	case "reauth_unavailable":
		return "This account has no way to confirm it's you here, so this can't be done. Ask your administrator."
	case "token_admin_expiry":
		return "A token with repo:admin must expire within 90 days. Pick an expiry date and try again."
	case "token_admin_targets":
		return "A token with repo:admin must name the repositories (owner/repo) or organizations it may administer."
	case "token_target":
		return "A token can only be limited to repositories you manage and organizations you own, and only with repo:admin. Nothing was created."
	case "token_admin_key":
		return "A token with repo:admin needs a signing key. Paste an SSH public key; its private key will sign each request."
	case "token_key_invalid":
		return "The signing key must be an Ed25519, ECDSA, 2048-bit RSA or hardware (sk-) SSH public key, like the contents of a .pub file."
	case "reauth_error":
		return "Couldn't check your password or code. Please try again."
	case "password_mismatch":
		return "The new passwords didn't match. Nothing was changed."
	case "password_too_short":
		return "The new password needs at least 8 characters. Nothing was changed."
	case "password_too_long":
		return "The new password can be at most 72 bytes. Nothing was changed."
	case "password_change_failed":
		return "Couldn't change your password. Please try again."
	case "sessions_revoke_failed":
		return "Couldn't sign out your other sessions. Please try again."
	case "verification_cooldown":
		return "Verification emails go out at most once a minute. Try again shortly."
	case "already_verified":
		return "Your email address is already verified."
	case "verification_unavailable":
		return "Verifying your email needs outgoing email, which the administrator hasn't configured."
	case "verification_failed":
		return "Couldn't send the verification email. Please try again later."
	case "google_not_configured":
		return "Google sign-in isn't set up on this instance."
	case "google_reauth_failed":
		return "Your password or two-factor code was incorrect. Nothing was changed."
	case "google_no_password":
		return "This account has no password to confirm it's you with, so its Google sign-in can't be changed here."
	case "google_link_invalid":
		return "That Google connection request expired or was already used. Start again."
	case "google_link_wrong_user":
		return "That Google connection was started from a different account. Nothing was connected."
	case "google_link_signed_out":
		return "You were signed out before Google sent you back. Sign in and connect again."
	case "google_link_cancelled":
		return "Google sign-in was cancelled. Nothing was connected."
	case "google_link_unverified":
		return "Google hasn't verified that Google account's email address, so it can't be connected."
	case "google_link_taken":
		return "That Google account is already connected to a different Cloudzilla account."
	case "google_already_connected":
		return "Your account is already connected to a Google account. Disconnect it before connecting another."
	case "google_not_connected":
		return "Your account isn't connected to Google."
	case "google_link_failed":
		return "Couldn't connect Google. Please try again."
	}
	return "Something went wrong."
}

// settingsNoticeMessage returns "" for an unknown code, so nothing renders.
func settingsNoticeMessage(code string) string {
	switch code {
	case "google_connected":
		return "Google account connected. You can now sign in with Google."
	case "google_disconnected":
		return "Google account disconnected. Sign in with your password from now on."
	}
	return ""
}

const codeThemePreview = `// Greet welcomes name and counts their unread messages.
func Greet(name string, unread int) string {
	if name == "" {
		return "Hello, world!"
	}
	return fmt.Sprintf("Hello, %s! You have %d new messages.", name, unread)
}`

func codeThemePreviewHTML() template.HTML {
	return highlight.Block("go", "", codeThemePreview)
}

func codeThemeOptions(themes []highlight.Theme) []components.SelectMenuOption {
	out := make([]components.SelectMenuOption, len(themes))
	for i, t := range themes {
		out[i] = components.SelectMenuOption{Value: t.ID, Label: t.Name, Swatch: highlight.Swatch(t.ID)}
	}
	return out
}

func avatarInitials(name, username string) string {
	src := strings.TrimSpace(name)
	if src == "" {
		src = username
	}
	if src == "" {
		return "?"
	}
	parts := strings.Fields(src)
	if len(parts) >= 2 {
		return strings.ToUpper(parts[0][:1] + parts[1][:1])
	}
	if len(src) >= 2 {
		return strings.ToUpper(src[:2])
	}
	return strings.ToUpper(src[:1])
}
