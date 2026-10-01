package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/concurrency"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"golang.org/x/crypto/bcrypt"
)

const (
	reauthFailureLimit  = 5
	reauthFailureWindow = 15 * time.Minute
	oneTimeCodeTTL      = 10 * time.Minute
	emailCodeGap        = time.Minute
	emailCodesPerHour   = 5
	providerSignInTTL   = 5 * time.Minute
)

var (
	ErrReauthFailed      = errors.New("the password or two-factor code is incorrect")
	ErrReauthNoPassword  = errors.New("this account has no password to confirm the change with")
	ErrReauthThrottled   = errors.New("too many incorrect passwords or codes; try again later")
	ErrReauthUnavailable = errors.New("this account has no way to confirm the change")
	ErrEmailCodeCooldown = errors.New("confirmation codes go out at most once a minute and 5 times an hour")
	ErrEmailCodeNeedless = errors.New("this account confirms changes with its password or two-factor code")
	ErrSignInMismatch    = errors.New("that sign-in doesn't confirm this account")
)

// Confirmation is what the user gave to confirm a sensitive action. OneTimeCode
// is a code that was emailed, or that a fresh provider sign-in left in the browser.
type Confirmation struct {
	Password    string
	Code        string
	OneTimeCode string
}

// Factors are what an account confirms sensitive actions with. Accounts with a
// password or 2FA use those. Accounts with neither use whichever of the rest
// apply: their LDAP password, a fresh sign-in with Google or SAML, or an emailed code.
type Factors struct {
	Password  bool
	Code      bool
	Directory bool
	Provider  string
	Email     bool
}

// Passwordless reports whether the account confirms without a password or 2FA.
func (f Factors) Passwordless() bool { return !f.Password && !f.Code }

// None reports whether the account has no way to confirm at all.
func (f Factors) None() bool {
	return f.Passwordless() && !f.Directory && f.Provider == "" && !f.Email
}

// ReauthService confirms sensitive actions with the account's own factors, so a
// stolen session alone can't add a way in that outlives it. Every check claims
// an attempt from a per-user window kept in the database, so a session can't
// guess the password or code, however many instances serve it.
type ReauthService struct {
	users         *store.UserStore
	totp          *TOTPService
	email         *EmailService
	states        *store.OAuthStateStore
	sso           *SSOService
	googleEnabled bool
}

func NewReauthService(users *store.UserStore, totp *TOTPService) *ReauthService {
	return &ReauthService{users: users, totp: totp}
}

// Without it, accounts with no password or 2FA can't confirm with an emailed code.
func (s *ReauthService) WithEmailCodes(e *EmailService) *ReauthService {
	s.email = e
	return s
}

// Without it, accounts with no password or 2FA can't confirm by signing in
// again with Google, SAML or their LDAP password.
func (s *ReauthService) WithProviderSignIn(states *store.OAuthStateStore, sso *SSOService, googleEnabled bool) *ReauthService {
	s.states, s.sso, s.googleEnabled = states, sso, googleEnabled
	return s
}

// Confirm checks c against the factors userID has; see Factors. An account
// with no way to confirm is refused, never let through.
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
	f, bindDN, err := s.factorsOf(ctx, u)
	if err != nil {
		return nil, err
	}
	switch {
	case requirePassword && !f.Password:
		return nil, ErrReauthNoPassword
	case f.None():
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
	if f.Passwordless() {
		ok = f.Directory && c.Password != "" && s.sso.CheckLDAPPassword(ctx, bindDN, c.Password) == nil
		if !ok && (f.Provider != "" || f.Email) {
			if ok, err = s.spendOneTimeCode(ctx, userID, c.OneTimeCode); err != nil {
				return nil, err
			}
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
	f, _, err := s.factorsOf(ctx, u)
	return f, err
}

// factorsOf also returns the LDAP bind DN a Directory account confirms against.
func (s *ReauthService) factorsOf(ctx context.Context, u *model.User) (Factors, string, error) {
	f := Factors{Password: u.PasswordHash != "", Code: u.TOTPEnabled}
	if !f.Passwordless() {
		return f, "", nil
	}
	var ssoProvider, ssoID string
	if s.sso != nil {
		var err error
		if ssoProvider, ssoID, err = s.users.SSOLink(ctx, u.ID); err != nil {
			return Factors{}, "", err
		}
	}
	switch {
	case ssoProvider == "ldap" && ssoID != "" && s.sso.ProviderEnabled(ctx, "ldap"):
		f.Directory = true
	case ssoProvider == "saml" && s.sso.ProviderEnabled(ctx, "saml"):
		f.Provider = "saml"
	case u.OAuthProvider == "google" && s.googleEnabled && s.states != nil:
		f.Provider = "google"
	}
	// An LDAP account's address is made up (uid@ldap.local), so it can't receive a code.
	f.Email = !f.Directory && u.Email != "" && s.email != nil && s.email.Enabled()
	return f, ssoID, nil
}

// SendEmailCode mails userID a one-time code to confirm a sensitive action
// with, when the account has no password or 2FA. The code replaces any earlier
// one; codes go out at most once a minute and five times an hour.
func (s *ReauthService) SendEmailCode(ctx context.Context, userID int64) error {
	u, err := s.users.GetByIDWithTOTP(ctx, userID)
	if err != nil {
		return err
	}
	f, _, err := s.factorsOf(ctx, u)
	if err != nil {
		return err
	}
	if !f.Email {
		if !f.Passwordless() {
			return ErrEmailCodeNeedless
		}
		return ErrReauthUnavailable
	}
	code, hash, err := newOneTimeCode()
	if err != nil {
		return err
	}
	issued, err := s.users.IssueReauthCode(ctx, userID, hash, oneTimeCodeTTL, emailCodeGap, emailCodesPerHour, time.Hour)
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

func newOneTimeCode() (code, hash string, err error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", "", fmt.Errorf("generate confirmation code: %w", err)
	}
	code = fmt.Sprintf("%06d", n.Int64())
	h, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.MinCost)
	if err != nil {
		return "", "", fmt.Errorf("hash confirmation code: %w", err)
	}
	return code, string(h), nil
}

func emailCodeMessage(username, code string) (subject, body string) {
	return "Your Cloudzilla confirmation code",
		fmt.Sprintf("<p>Enter <strong>%s</strong> to confirm a change to the Cloudzilla account <strong>@%s</strong>. It works once, for 10 minutes.</p>"+
			"<p>If you didn't ask for it, someone signed in as you is trying to change who can get in. Choose <em>Sign out other sessions</em> under Account settings and tell your administrator.</p>",
			code, html.EscapeString(username))
}

// spendOneTimeCode reports whether code is userID's live one-time code, using it up if so.
func (s *ReauthService) spendOneTimeCode(ctx context.Context, userID int64, code string) (bool, error) {
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

// BeginProviderSignIn starts a fresh sign-in with provider for userID, an
// account that confirms that way, and returns the state the provider hands back.
func (s *ReauthService) BeginProviderSignIn(ctx context.Context, userID int64, provider string) (string, time.Time, error) {
	f, err := s.Factors(ctx, userID)
	if err != nil {
		return "", time.Time{}, err
	}
	if f.Provider == "" || f.Provider != provider {
		return "", time.Time{}, ErrSignInMismatch
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, fmt.Errorf("generate sign-in state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(raw)
	expiresAt, err := s.states.Put(ctx, userID, model.OAuthStatePurposeReauth, sha256HexOf(state), providerSignInTTL)
	if err != nil {
		return "", time.Time{}, err
	}
	return state, expiresAt, nil
}

// FinishGoogleSignIn spends state and, when Google signed in the account that
// started it, returns a one-time code for the browser that finished it.
func (s *ReauthService) FinishGoogleSignIn(ctx context.Context, state, googleID string) (int64, string, error) {
	return s.finishSignIn(ctx, state, func(u *model.User) bool {
		return u.OAuthProvider == "google" && googleID != "" && u.OAuthID == googleID
	})
}

// FinishSAMLSignIn is FinishGoogleSignIn for a SAML response that signed in accountID.
func (s *ReauthService) FinishSAMLSignIn(ctx context.Context, state string, accountID int64) (int64, string, error) {
	return s.finishSignIn(ctx, state, func(u *model.User) bool { return u.ID == accountID })
}

func (s *ReauthService) finishSignIn(ctx context.Context, state string, signedIn func(*model.User) bool) (int64, string, error) {
	// Spent before anything can refuse, so no attempt leaves it redeemable.
	userID, err := s.states.Take(ctx, sha256HexOf(state), model.OAuthStatePurposeReauth)
	if err != nil {
		return 0, "", ErrSignInMismatch
	}
	u, err := s.users.GetByIDWithTOTP(ctx, userID)
	if err != nil {
		return 0, "", err
	}
	if !signedIn(u) {
		slog.Warn("provider sign-in for another account", "user_id", userID)
		return 0, "", ErrSignInMismatch
	}
	code, hash, err := newOneTimeCode()
	if err != nil {
		return 0, "", err
	}
	if err := s.users.StoreReauthCode(ctx, userID, hash, oneTimeCodeTTL); err != nil {
		return 0, "", err
	}
	return userID, code, nil
}

// NotifyTokenAdmin mails userID that their personal access token tokenName made
// an administrative change, which a repo:admin token does without a prompt.
func (s *ReauthService) NotifyTokenAdmin(userID int64, tokenName, change string) {
	if s.email == nil {
		return
	}
	concurrency.Go("token_admin.notice", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		u, err := s.users.GetByID(ctx, userID)
		if err != nil {
			slog.Error("token admin notice: load user", "user_id", userID, "error", err)
			return
		}
		body := fmt.Sprintf("<p>Your personal access token <strong>%s</strong> was used for <code>%s</code> on the Cloudzilla account <strong>@%s</strong>.</p>"+
			"<p>If that wasn't you or your scripts, revoke the token under Account settings → Access tokens.</p>",
			html.EscapeString(tokenName), html.EscapeString(change), html.EscapeString(u.Username))
		if err := s.email.SendSecurityNotice(u, "A personal access token made an admin change", body); err != nil {
			slog.Error("token admin notice: send", "user_id", userID, "error", err)
		}
	})
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
