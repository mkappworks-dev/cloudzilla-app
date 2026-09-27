package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

const signupLinkTTL = 24 * time.Hour

var ErrSignupTokenUnusable = store.ErrSignupTokenUnusable

// SignupMailer sends the two emails of email-first signup.
type SignupMailer interface {
	Enabled() bool
	SendSignupLink(to, link string) error
	SendAccountExists(to, loginURL string) error
}

// SignupService runs email-first signup: an account is created only from a
// link mailed to its address, so /register never reveals whether one exists.
type SignupService struct {
	tokens  *store.SignupTokenStore
	users   *store.UserStore
	mailer  SignupMailer
	baseURL string
}

// baseURL is the public site URL the emailed links point at.
func NewSignupService(tokens *store.SignupTokenStore, users *store.UserStore, mailer SignupMailer, baseURL string) *SignupService {
	return &SignupService{tokens: tokens, users: users, mailer: mailer, baseURL: strings.TrimRight(baseURL, "/")}
}

func (s *SignupService) Enabled() bool {
	return s.mailer.Enabled()
}

// Request sends the address a signup link, or a sign-in reminder if it already
// has an account. It sends nothing if the address was mailed in the last 5 minutes.
func (s *SignupService) Request(ctx context.Context, email string) error {
	token, err := newSignupToken()
	if err != nil {
		return err
	}
	issued, err := s.tokens.Issue(ctx, email, hashSignupToken(token), time.Now().Add(signupLinkTTL))
	if err != nil || !issued {
		return err
	}
	_, err = s.users.GetByEmail(ctx, email)
	switch {
	case err == nil:
		return s.mailer.SendAccountExists(email, s.baseURL+"/login")
	case errors.Is(err, sql.ErrNoRows):
		return s.mailer.SendSignupLink(email, s.baseURL+"/register/complete/"+token)
	default:
		return err
	}
}

func (s *SignupService) GetUsable(ctx context.Context, token string) (*model.SignupToken, error) {
	return s.tokens.GetUsableByHash(ctx, hashSignupToken(token))
}

// Complete creates the account for a usable link. It returns
// ErrSignupTokenUnusable if the link was used, expired, or its email
// registered since it was loaded.
func (s *SignupService) Complete(ctx context.Context, token, username, password string) (*model.User, error) {
	if err := ValidateOwnerName(username); err != nil {
		return nil, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	u := &model.User{Username: username, PasswordHash: hash}
	if err := s.users.CreateFromSignupToken(ctx, u, hashSignupToken(token)); err != nil {
		return nil, err
	}
	return u, nil
}

func newSignupToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate signup token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func hashSignupToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
