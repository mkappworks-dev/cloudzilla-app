package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/concurrency"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

const (
	EmailVerificationTTL      = 24 * time.Hour
	EmailVerificationCooldown = time.Minute
	verificationTokenBytes    = 32
)

var (
	ErrEmailVerificationUnavailable = errors.New("email verification needs outgoing email, which the administrator has not configured")
	ErrEmailAlreadyVerified         = store.ErrEmailAlreadyVerified
	ErrVerificationCooldown         = store.ErrVerificationCooldown
	ErrNoSuchUserEmail              = errors.New("no user has that username and email")
)

// EmailVerificationService proves a user controls their address with a
// single-use link sent to it.
type EmailVerificationService struct {
	store   *store.EmailVerificationStore
	users   *store.UserStore
	email   *EmailService
	baseURL string
}

func NewEmailVerificationService(s *store.EmailVerificationStore, users *store.UserStore, email *EmailService, baseURL string) *EmailVerificationService {
	return &EmailVerificationService{store: s, users: users, email: email, baseURL: strings.TrimRight(baseURL, "/")}
}

// Available is false without SMTP: no link could reach the user, so none is issued.
func (s *EmailVerificationService) Available() bool {
	return s.email.Enabled()
}

// Send issues userID a link for their current address, revoking earlier ones,
// and mails it in the background so a slow SMTP server doesn't hold up the
// request. A failed delivery is only logged; the token stays, since the server
// may have accepted the message before reporting the error.
func (s *EmailVerificationService) Send(ctx context.Context, userID int64) error {
	if !s.Available() {
		return ErrEmailVerificationUnavailable
	}
	raw, hash, err := newVerificationToken()
	if err != nil {
		return err
	}
	email, username, err := s.store.Issue(ctx, userID, hash, EmailVerificationTTL, EmailVerificationCooldown)
	if err != nil {
		return err
	}
	link := s.baseURL + "/verify-email?" + url.Values{"token": {raw}}.Encode()
	concurrency.Go("email_verification.send", func() {
		if err := s.email.Send(email, "Verify your email address", verificationEmailBody(link, username, email)); err != nil {
			slog.Error("send verification email", "user_id", userID, "error", err)
		}
	})
	return nil
}

// notifyAddressChanged tells the old address, whose owner may not have made the
// change. A failure is only logged: the change itself already happened.
func (s *EmailVerificationService) notifyAddressChanged(username, oldEmail, newEmail string) {
	if !s.Available() {
		return
	}
	concurrency.Go("email_change.notice", func() {
		subject := "The email address on your Cloudzilla account changed"
		body := fmt.Sprintf(`<p>The email address of the Cloudzilla account <strong>@%s</strong> changed from %s to <strong>%s</strong>.</p>
<p>If you didn't change it, someone else can sign in as you. Sign in with the new address, choose <em>Sign out other sessions</em> under Account settings, and tell your administrator.</p>`,
			html.EscapeString(username), html.EscapeString(oldEmail), html.EscapeString(newEmail))
		if err := s.email.Send(oldEmail, subject, body); err != nil {
			slog.Error("email change notice", "username", username, "error", err)
		}
	})
}

// LinkPending reports whether a live link for userID's current address is out.
func (s *EmailVerificationService) LinkPending(ctx context.Context, userID int64) (bool, error) {
	return s.store.LinkPending(ctx, userID)
}

// Check reports what Verify would do with rawToken, without spending it.
func (s *EmailVerificationService) Check(ctx context.Context, rawToken string) (model.EmailVerificationLink, error) {
	hash, ok := hashVerificationToken(rawToken)
	if !ok {
		return model.EmailVerificationLink{State: model.EmailVerificationInvalid}, nil
	}
	return s.store.Lookup(ctx, hash)
}

// Verify spends rawToken. The user is returned only when it verified their address.
func (s *EmailVerificationService) Verify(ctx context.Context, rawToken string) (model.EmailVerificationState, *model.User, error) {
	hash, ok := hashVerificationToken(rawToken)
	if !ok {
		return model.EmailVerificationInvalid, nil, nil
	}
	return s.store.Consume(ctx, hash)
}

// MarkVerified is the superadmin's manual override. Naming the email as well as
// the user means an address changed since the admin checked it is not verified.
func (s *EmailVerificationService) MarkVerified(ctx context.Context, username, email string) (*model.User, error) {
	u, err := s.users.GetByUsername(ctx, username)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoSuchUserEmail
	}
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(u.Email, email) {
		return nil, ErrNoSuchUserEmail
	}
	ok, err := s.users.MarkEmailVerified(ctx, u.ID, email)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNoSuchUserEmail
	}
	return u, nil
}

func newVerificationToken() (raw, hash string, err error) {
	b := make([]byte, verificationTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate verification token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	hash, _ = hashVerificationToken(raw)
	return raw, hash, nil
}

// Anything but a well-formed token is refused before it reaches the database.
func hashVerificationToken(raw string) (string, bool) {
	if len(raw) != base64.RawURLEncoding.EncodedLen(verificationTokenBytes) {
		return "", false
	}
	if _, err := base64.RawURLEncoding.DecodeString(raw); err != nil {
		return "", false
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:]), true
}

// Naming the account lets the owner of the address refuse to vouch for an
// account someone else registered with it.
func verificationEmailBody(link, username, email string) string {
	href := html.EscapeString(link)
	return fmt.Sprintf(`<p>Confirm that %s belongs to the Cloudzilla account <strong>%s</strong>:</p>
<p><a href="%s">Verify email address</a></p>
<p>Or open this link: %s</p>
<p>The link works once and expires in 24 hours. If you didn't create or change the account %s, ignore this email.</p>`,
		html.EscapeString(email), html.EscapeString(username), href, href, html.EscapeString(username))
}
