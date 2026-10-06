package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

const (
	PasswordResetEmailTTL = time.Hour
	// Links an admin or the CLI issues are passed on by hand, which takes longer than opening an email.
	PasswordResetManualTTL = 24 * time.Hour
	PasswordResetCooldown  = 5 * time.Minute
)

var (
	ErrPasswordResetUnavailable = errors.New("password reset emails need outgoing email, which the administrator has not configured")
	ErrPasswordResetNoPassword  = errors.New("this account signs in without a password, so it has no password to reset")
	ErrPasswordResetExpired     = errors.New("this password reset link has expired")
	ErrPasswordResetInvalid     = errors.New("this password reset link isn't valid")
)

// PasswordResetService lets someone who forgot their password set a new one
// from a single-use link, mailed to the account's address or issued by an admin.
type PasswordResetService struct {
	store   *store.PasswordResetStore
	users   *store.UserStore
	reauth  *ReauthService
	email   *EmailService
	baseURL string
}

func NewPasswordResetService(s *store.PasswordResetStore, users *store.UserStore, reauth *ReauthService, email *EmailService, baseURL string) *PasswordResetService {
	return &PasswordResetService{store: s, users: users, reauth: reauth, email: email, baseURL: strings.TrimRight(baseURL, "/")}
}

// Available is false without SMTP, when only IssueLink can start a reset.
func (s *PasswordResetService) Available() bool {
	return s.email.Enabled()
}

// Request mails the account with this address a reset link, or, if it has no
// password, a note saying how it signs in. It sends nothing for an unknown
// address, if the account was mailed in the last 5 minutes, or while a link an
// admin issued is still usable. A failed delivery still counts, since the
// server may have accepted the message before failing.
func (s *PasswordResetService) Request(ctx context.Context, email string) error {
	if !s.Available() {
		return ErrPasswordResetUnavailable
	}
	u, err := s.users.GetByEmail(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var raw, hash string
	if u.PasswordHash != "" {
		if raw, hash, err = newLinkToken(); err != nil {
			return err
		}
	}
	// The link is bound to the address Issue read under lock, which an email change may have replaced since the lookup.
	to, err := s.store.Issue(ctx, u.ID, hash, model.PasswordResetByEmail, PasswordResetEmailTTL, PasswordResetCooldown)
	if errors.Is(err, store.ErrPasswordResetCooldown) {
		return nil
	}
	if err != nil {
		return err
	}
	if raw == "" {
		subject, body := passwordlessResetNote(u)
		return s.email.Send(to, subject, body)
	}
	return s.email.Send(to, "Reset your Cloudzilla password", passwordResetEmailBody(s.linkURL(raw), u.Username))
}

// IssueLink returns a 24-hour link for userID instead of mailing it, replacing
// any outstanding one. issuedBy is model.PasswordResetByAdmin or PasswordResetByCLI.
func (s *PasswordResetService) IssueLink(ctx context.Context, userID int64, issuedBy string) (string, error) {
	if issuedBy != model.PasswordResetByAdmin && issuedBy != model.PasswordResetByCLI {
		return "", fmt.Errorf("password reset issue link: unknown issuer %q", issuedBy)
	}
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return "", err
	}
	// A local password on an SSO account would survive its removal at the identity provider.
	if u.PasswordHash == "" {
		return "", ErrPasswordResetNoPassword
	}
	raw, hash, err := newLinkToken()
	if err != nil {
		return "", err
	}
	if _, err := s.store.Issue(ctx, userID, hash, issuedBy, PasswordResetManualTTL, 0); err != nil {
		return "", err
	}
	return s.linkURL(raw), nil
}

func (s *PasswordResetService) linkURL(raw string) string {
	return s.baseURL + "/auth/password/reset/" + raw
}

// Check reports the account rawToken would reset, without spending it.
func (s *PasswordResetService) Check(ctx context.Context, rawToken string) (model.PasswordResetLink, error) {
	hash, ok := hashLinkToken(rawToken)
	if !ok {
		return model.PasswordResetLink{State: model.PasswordResetInvalid}, nil
	}
	return s.store.Lookup(ctx, hash)
}

// Reset spends rawToken to set newPassword and sign the account out everywhere.
// An account with 2FA must also give a TOTP or backup code as code. A failed
// check leaves the link usable. It returns the user and who issued the link.
func (s *PasswordResetService) Reset(ctx context.Context, rawToken, newPassword, code string) (*model.User, string, error) {
	switch {
	case len(newPassword) < MinPasswordLen:
		return nil, "", ErrPasswordTooShort
	case len(newPassword) > MaxPasswordBytes:
		return nil, "", ErrPasswordTooLong
	}
	link, err := s.Check(ctx, rawToken)
	if err != nil {
		return nil, "", err
	}
	if err := linkStateError(link.State); err != nil {
		return nil, "", err
	}
	// Without this, someone holding only the mailbox could sign the owner out everywhere.
	if link.TOTPEnabled {
		if err := s.reauth.CheckSecondFactor(ctx, link.UserID, code, code); err != nil {
			return nil, "", err
		}
	}
	passwordHash, err := hashPassword(newPassword)
	if err != nil {
		return nil, "", err
	}
	tokenHash, _ := hashLinkToken(rawToken)
	state, u, issuedBy, err := s.store.Consume(ctx, tokenHash, passwordHash)
	if err != nil {
		return nil, "", err
	}
	if err := linkStateError(state); err != nil {
		return nil, "", err
	}
	notifySecurityChange(s.email, s.users, u.ID, "password_reset", passwordResetNotice(s.baseURL))
	return u, issuedBy, nil
}

func linkStateError(state model.PasswordResetState) error {
	switch state {
	case model.PasswordResetPending, model.PasswordResetDone:
		return nil
	case model.PasswordResetExpired:
		return ErrPasswordResetExpired
	}
	return ErrPasswordResetInvalid
}

func passwordResetEmailBody(link, username string) string {
	href := html.EscapeString(link)
	return fmt.Sprintf(`<p>Someone asked to reset the password of the Cloudzilla account <strong>@%s</strong>.</p>
<p><a href="%s">Choose a new password</a></p>
<p>Or open this link: %s</p>
<p>The link works once and expires in 1 hour. If you didn't ask for this, ignore this email. Your password hasn't changed.</p>`,
		html.EscapeString(username), href, href)
}

func passwordlessResetNote(u *model.User) (subject, body string) {
	method := "single sign-on"
	if u.OAuthProvider == "google" {
		method = "Google"
	}
	return "Your Cloudzilla account has no password",
		fmt.Sprintf(`<p>Someone asked to reset the password of the Cloudzilla account <strong>@%s</strong>.</p>
<p>That account signs in with %s, so it has no password to reset. Sign in with %s instead.</p>
<p>If you didn't ask for this, ignore this email.</p>`,
			html.EscapeString(u.Username), method, method)
}

func passwordResetNotice(baseURL string) func(username string) (subject, body string) {
	return func(username string) (string, string) {
		settings := html.EscapeString(baseURL + "/settings")
		return "Your Cloudzilla password was reset",
			fmt.Sprintf(`<p>The password of the Cloudzilla account <strong>@%s</strong> was reset from a password reset link, and every session signed in to it was signed out.</p>
<p>Personal access tokens, SSH keys and authorized apps still work. Review them under <a href="%s#tokens">Access tokens</a>, <a href="%s#ssh-keys">SSH keys</a> and <a href="%s#oauth-apps">Applications</a>.</p>
<p>If you didn't reset it, someone else can sign in as you. Tell your administrator.</p>`,
				html.EscapeString(username), settings, settings, settings)
	}
}
