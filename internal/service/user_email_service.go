package service

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/url"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/concurrency"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// MaxAdditionalEmails bounds the unverified rows one account can park on other people's addresses.
const MaxAdditionalEmails = 10

var (
	ErrTooManyEmails         = store.ErrTooManyEmails
	ErrNoSuchAdditionalEmail = errors.New("no such additional email address")
)

// WithAdditionalEmails enables additional addresses; reauth confirms adding and removing one.
func (s *EmailVerificationService) WithAdditionalEmails(e *store.UserEmailStore, reauth *ReauthService) *EmailVerificationService {
	s.extra = e
	s.reauth = reauth
	return s
}

// AddAddress claims email for userID and mails it a link. Without SMTP nothing is stored, since no link could be sent.
func (s *EmailVerificationService) AddAddress(ctx context.Context, userID int64, email string, confirm Confirmation) error {
	if !s.Available() {
		return ErrEmailVerificationUnavailable
	}
	email = strings.TrimSpace(email)
	if !emailRe.MatchString(email) {
		return ErrInvalidEmail
	}
	if _, err := s.reauth.Confirm(ctx, userID, confirm); err != nil {
		return err
	}
	// The unique index would refuse a taken address too; this keeps a taken one from costing a token.
	if _, err := s.users.GetByEmail(ctx, email); err == nil {
		return ErrEmailTaken
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	raw, hash, err := newLinkToken()
	if err != nil {
		return err
	}
	e, username, err := s.extra.Add(ctx, userID, email, hash, EmailVerificationTTL, EmailVerificationCooldown, MaxAdditionalEmails)
	if err != nil {
		return err
	}
	link := s.baseURL + "/verify-email?" + url.Values{"token": {raw}}.Encode()
	concurrency.Go("user_email.send", func() {
		if err := s.email.Send(e.Email, "Verify your email address", verificationEmailBody(link, username, e.Email)); err != nil {
			slog.Error("send additional email verification", "user_id", userID, "error", err)
		}
	})
	return nil
}

// RemoveAddress drops one of userID's additional addresses, which stops it crediting commits.
func (s *EmailVerificationService) RemoveAddress(ctx context.Context, userID, id int64, confirm Confirmation) error {
	if _, err := s.reauth.Confirm(ctx, userID, confirm); err != nil {
		return err
	}
	ok, err := s.extra.Remove(ctx, userID, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNoSuchAdditionalEmail
	}
	return nil
}

func (s *EmailVerificationService) Addresses(ctx context.Context, userID int64) ([]model.UserEmail, error) {
	return s.extra.List(ctx, userID)
}

// VerifyAddress spends rawToken for an additional address. The row is returned only when it verified one.
func (s *EmailVerificationService) VerifyAddress(ctx context.Context, rawToken string) (model.EmailVerificationState, *model.UserEmail, string, error) {
	hash, ok := hashLinkToken(rawToken)
	if !ok {
		return model.EmailVerificationInvalid, nil, "", nil
	}
	return s.extra.Consume(ctx, hash)
}
