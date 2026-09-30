package pages

import (
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

var emailDigestLabels = map[string]string{
	model.EmailDigestImmediate: "Immediately",
	model.EmailDigestDaily:     "Daily digest",
	model.EmailDigestWeekly:    "Weekly digest",
	model.EmailDigestNever:     "Never",
}

func settingsErrorMessage(code string) string {
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
