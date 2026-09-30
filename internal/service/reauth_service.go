package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"golang.org/x/crypto/bcrypt"
)

const (
	reauthFailureLimit  = 5
	reauthFailureWindow = 15 * time.Minute
)

var (
	ErrReauthFailed     = errors.New("the password or two-factor code is incorrect")
	ErrReauthNoPassword = errors.New("this account has no password to confirm the change with")
	ErrReauthThrottled  = errors.New("too many incorrect passwords or codes; try again later")
)

// Confirmation is what the user typed to confirm a sensitive action.
type Confirmation struct {
	Password string
	Code     string
}

// ReauthService confirms sensitive actions with the account's own factors, so a
// stolen session alone can't add a way in that outlives it. Every check claims
// an attempt from a per-user window kept in the database, so a session can't
// guess the password or code, however many instances serve it.
type ReauthService struct {
	users *store.UserStore
	totp  *TOTPService
}

func NewReauthService(users *store.UserStore, totp *TOTPService) *ReauthService {
	return &ReauthService{users: users, totp: totp}
}

// Confirm checks c against the factors userID has: the password if the account
// has one, and the TOTP code when 2FA is on. An account with neither has nothing
// to confirm with and passes; ConfirmWithPassword refuses it instead.
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
	if u.PasswordHash == "" {
		if requirePassword {
			return nil, ErrReauthNoPassword
		}
		if !u.TOTPEnabled {
			return u, nil
		}
	}
	if err := s.claim(ctx, userID); err != nil {
		return nil, err
	}
	ok := u.PasswordHash == "" || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(c.Password)) == nil
	if u.TOTPEnabled && !s.totpCodeValid(u, c.Code) {
		ok = false
	}
	if !ok {
		return nil, ErrReauthFailed
	}
	s.release(ctx, userID)
	return u, nil
}

// Factors reports what userID confirms sensitive actions with.
func (s *ReauthService) Factors(ctx context.Context, userID int64) (password, code bool, err error) {
	u, err := s.users.GetByIDWithTOTP(ctx, userID)
	if err != nil {
		return false, false, err
	}
	return u.PasswordHash != "", u.TOTPEnabled, nil
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
