package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"golang.org/x/crypto/bcrypt"
)

const (
	reauthFailureLimit  = 5
	reauthFailureWindow = 15 * time.Minute
	emailCodeTTL        = 10 * time.Minute
	emailCodeCooldown   = time.Minute
)

var (
	ErrReauthFailed      = errors.New("the password or two-factor code is incorrect")
	ErrReauthNoPassword  = errors.New("this account has no password to confirm the change with")
	ErrReauthThrottled   = errors.New("too many incorrect passwords or codes; try again later")
	ErrReauthUnavailable = errors.New("this account has no password, two-factor app or email to confirm the change with")
	ErrEmailCodeCooldown = errors.New("a confirmation code was emailed less than a minute ago")
	ErrEmailCodeNeedless = errors.New("this account confirms changes with its password or two-factor code")
)

// Confirmation is what the user typed to confirm a sensitive action.
type Confirmation struct {
	Password  string
	Code      string
	EmailCode string
}

// Factors are what an account confirms sensitive actions with. Email is an
// emailed code, used only by accounts that have neither of the others.
type Factors struct {
	Password bool
	Code     bool
	Email    bool
}

// ReauthService confirms sensitive actions with the account's own factors, so a
// stolen session alone can't add a way in that outlives it. Every check claims
// an attempt from a per-user window kept in the database, so a session can't
// guess the password or code, however many instances serve it.
type ReauthService struct {
	users *store.UserStore
	totp  *TOTPService
	email *EmailService
}

func NewReauthService(users *store.UserStore, totp *TOTPService) *ReauthService {
	return &ReauthService{users: users, totp: totp}
}

// Without it, an account with no password or 2FA has nothing to confirm with,
// and its sensitive actions are refused.
func (s *ReauthService) WithEmailCodes(e *EmailService) *ReauthService {
	s.email = e
	return s
}

// Confirm checks c against the factors userID has: the password if the account
// has one, the TOTP code when 2FA is on, and otherwise an emailed code. An
// account with none of these can't confirm, so it's refused.
func (s *ReauthService) Confirm(ctx context.Context, userID int64, c Confirmation) (*model.User, error) {
	return s.confirm(ctx, userID, c, false)
}

func (s *ReauthService) ConfirmWithPassword(ctx context.Context, userID int64, c Confirmation) (*model.User, error) {
	return s.confirm(ctx, userID, c, true)
}

func (s *ReauthService) confirm(ctx context.Context, userID int64, c Confirmation, requirePassword bool) (*model.User, error) {
	u, err := s.users.GetByIDWithTOTP(ctx, userID)
	if err != nil {
		return nil, err
	}
	f := s.factorsOf(u)
	switch {
	case requirePassword && !f.Password:
		return nil, ErrReauthNoPassword
	case !f.Password && !f.Code && !f.Email:
		return nil, ErrReauthUnavailable
	}
	if err := s.claim(ctx, userID); err != nil {
		return nil, err
	}
	ok := true
	if f.Password {
		ok = bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(c.Password)) == nil
	}
	if f.Code && !s.totpCodeValid(u, c.Code) {
		ok = false
	}
	if f.Email {
		ok, err = s.spendEmailCode(ctx, userID, c.EmailCode)
		if err != nil {
			return nil, err
		}
	}
	if !ok {
		return nil, ErrReauthFailed
	}
	s.release(ctx, userID)
	return u, nil
}

// Factors reports what userID confirms sensitive actions with.
func (s *ReauthService) Factors(ctx context.Context, userID int64) (Factors, error) {
	u, err := s.users.GetByIDWithTOTP(ctx, userID)
	if err != nil {
		return Factors{}, err
	}
	return s.factorsOf(u), nil
}

func (s *ReauthService) factorsOf(u *model.User) Factors {
	f := Factors{Password: u.PasswordHash != "", Code: u.TOTPEnabled}
	f.Email = !f.Password && !f.Code && u.Email != "" && s.email != nil && s.email.Enabled()
	return f
}

// SendEmailCode mails userID a one-time code to confirm a sensitive action
// with, when the account has no password or 2FA. The code replaces any earlier
// one; codes go out at most once a minute.
func (s *ReauthService) SendEmailCode(ctx context.Context, userID int64) error {
	u, err := s.users.GetByIDWithTOTP(ctx, userID)
	if err != nil {
		return err
	}
	if !s.factorsOf(u).Email {
		if u.PasswordHash != "" || u.TOTPEnabled {
			return ErrEmailCodeNeedless
		}
		return ErrReauthUnavailable
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return fmt.Errorf("generate confirmation code: %w", err)
	}
	code := fmt.Sprintf("%06d", n.Int64())
	hash, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.MinCost)
	if err != nil {
		return fmt.Errorf("hash confirmation code: %w", err)
	}
	issued, err := s.users.IssueReauthCode(ctx, userID, string(hash), emailCodeTTL, emailCodeCooldown)
	if err != nil {
		return err
	}
	if !issued {
		return ErrEmailCodeCooldown
	}
	subject, body := emailCodeMessage(u.Username, code)
	if err := s.email.SendSecurityNotice(u, subject, body); err != nil {
		return fmt.Errorf("send confirmation code: %w", err)
	}
	return nil
}

func emailCodeMessage(username, code string) (subject, body string) {
	return "Your Cloudzilla confirmation code",
		fmt.Sprintf("<p>Enter <strong>%s</strong> to confirm a change to the Cloudzilla account <strong>@%s</strong>. It works once, for 10 minutes.</p>"+
			"<p>If you didn't ask for it, someone signed in as you is trying to change who can get in. Choose <em>Sign out other sessions</em> under Account settings and tell your administrator.</p>",
			code, html.EscapeString(username))
}

// spendEmailCode reports whether code is userID's live emailed code, using it up if so.
func (s *ReauthService) spendEmailCode(ctx context.Context, userID int64, code string) (bool, error) {
	code = strings.TrimSpace(code)
	hash, err := s.users.LiveReauthCode(ctx, userID)
	if err != nil || hash == "" || code == "" {
		return false, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(code)) != nil {
		return false, nil
	}
	return s.users.SpendReauthCode(ctx, userID, hash)
}

// CheckSecondFactor verifies a sign-in's TOTP code, or else its backup code,
// against the same attempt window, so the code can't be guessed at /auth/2fa either.
func (s *ReauthService) CheckSecondFactor(ctx context.Context, userID int64, code, backupCode string) error {
	u, err := s.users.GetByIDWithTOTP(ctx, userID)
	if err != nil {
		return err
	}
	if err := s.claim(ctx, userID); err != nil {
		return err
	}
	ok := strings.TrimSpace(code) != "" && s.totpCodeValid(u, code)
	if !ok && strings.TrimSpace(backupCode) != "" {
		ok = s.totp.VerifyBackupCode(ctx, userID, strings.TrimSpace(backupCode)) == nil
	}
	if !ok {
		return ErrReauthFailed
	}
	s.release(ctx, userID)
	return nil
}

// An empty secret would still yield a predictable code, so it never verifies.
func (s *ReauthService) totpCodeValid(u *model.User, code string) bool {
	return u.TOTPSecret.String != "" && s.totp.Verify(u.TOTPSecret.String, strings.TrimSpace(code))
}

func (s *ReauthService) claim(ctx context.Context, userID int64) error {
	ok, err := s.users.ClaimReauthAttempt(ctx, userID, reauthFailureLimit, reauthFailureWindow)
	if err != nil {
		return err
	}
	if !ok {
		return ErrReauthThrottled
	}
	return nil
}

// A release that fails only leaves this success counted as a failure.
func (s *ReauthService) release(ctx context.Context, userID int64) {
	if err := s.users.ReleaseReauthAttempt(ctx, userID); err != nil {
		slog.Error("release reauth attempt", "user_id", userID, "error", err)
	}
}
