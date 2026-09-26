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
	case "totp_setup_failed":
		return "Couldn't start two-factor setup. Please try again."
	case "totp_missing_fields":
		return "Secret and verification code are required."
	case "totp_missing_code":
		return "Verification code is required."
	case "totp_invalid_code":
		return "Invalid verification code. Please try again."
	}
	return "Something went wrong."
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
