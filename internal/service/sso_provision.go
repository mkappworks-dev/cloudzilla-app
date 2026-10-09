package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// --------------------------------------------------------------------------
// User provisioning
// --------------------------------------------------------------------------

// findOrProvisionUser looks up a user by SSO provider + ID, or creates one.
// Returns the user and a signed JWT token.
//
// Email-based auto-linking is intentionally absent: silently linking an SSO
// identity to an existing password account would let any IdP operator claim
// any user's account by asserting their email address. Users who want to
// connect SSO to an existing account must do so from their account settings.
func (s *SSOService) findOrProvisionUser(ctx context.Context, provider, ssoID, username, email string, allowRegistration bool) (*model.User, string, error) {
	// Return the existing SSO-linked account if one exists.
	u, err := s.store.GetUserBySSO(ctx, provider, ssoID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, "", fmt.Errorf("sso user lookup: %w", err)
	}
	if err == nil {
		token, err := s.generateJWT(u)
		return u, token, err
	}

	// If an account with this email already exists (password or OAuth login),
	// refuse to auto-link. The user must link SSO explicitly from their settings.
	_, emailErr := s.users.GetByEmailWithRole(ctx, email)
	if emailErr == nil {
		return nil, "", fmt.Errorf("an account with this email already exists; sign in with your password and link SSO from account settings")
	}
	if !errors.Is(emailErr, sql.ErrNoRows) {
		return nil, "", fmt.Errorf("sso email lookup: %w", emailErr)
	}

	// New user — enforce the site registration policy.
	if !allowRegistration {
		return nil, "", ErrRegistrationDisabled
	}

	u, err = s.store.ProvisionSSOUser(ctx, sanitizeUsername(username), email, provider, ssoID)
	if err != nil {
		return nil, "", fmt.Errorf("provision sso user: %w", err)
	}
	token, err := s.generateJWT(u)
	return u, token, err
}

// generateJWT produces a signed JWT for an authenticated user.
func (s *SSOService) generateJWT(u *model.User) (string, error) {
	if u.Suspended() {
		return "", ErrAccountSuspended
	}
	claims := jwt.MapClaims{
		"sv":            u.SessionVersion,
		"sub":           u.ID,
		"username":      u.Username,
		"is_superadmin": u.IsSuperadmin,
		"exp":           time.Now().Add(s.cfg.JWTExpiry).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.cfg.JWTSecret))
}

// sanitizeUsername replaces non-alphanumeric characters with underscores and
// fits the result to the owner-name rule.
func sanitizeUsername(s string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(s) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			b.WriteRune(c)
		} else {
			b.WriteByte('_')
		}
	}
	return fitOwnerName(b.String(), "sso_user")
}
