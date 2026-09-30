package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrRegistrationDisabled = errors.New("registration is disabled")
	ErrLoginDisabled        = errors.New("login is currently disabled")
	ErrUsernameTaken        = store.ErrUsernameTaken
	ErrEmailTaken           = store.ErrEmailTaken
	ErrOrgNameTaken         = store.ErrOrgNameTaken
	ErrOAuthEmailUnverified = errors.New("the email address on this account has not been verified by the sign-in provider")
	ErrOAuthAccountExists   = errors.New("an account with this email already exists; sign in with your password")
	ErrPinLimit             = errors.New("pin limit reached (6)")
	ErrRepoNotFound         = errors.New("repository not found")
	ErrInvalidEmail         = errors.New("email must be a valid address")
	ErrSoleOrgOwner         = errors.New("you are the only owner of an organization")
	nonAlphanumRe           = regexp.MustCompile(`[^a-z0-9_-]`)
	emailRe                 = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

const MaxPinnedRepos = 6

// MaxPasswordBytes is bcrypt's input limit. Hashing longer passwords fails with
// bcrypt.ErrPasswordTooLong, so forms check this first to give a clear message.
const MaxPasswordBytes = 72

const MinPasswordLen = 8

var (
	ErrPasswordTooShort = errors.New("password must be at least 8 characters")
	ErrPasswordTooLong  = errors.New("password must be at most 72 bytes")
)

// Unknown emails are checked against this so login time doesn't reveal which emails have accounts.
var dummyPasswordHash = func() []byte {
	h, err := bcrypt.GenerateFromPassword([]byte("cloudzilla-dummy-password"), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return h
}()

// UserService manages user account operations including authentication and profile updates.
type UserService struct {
	store       *store.UserStore
	repos       *RepoService
	cfg         config.AuthConfig
	noreplyHost string
	verifier    *EmailVerificationService
	reauth      *ReauthService
	notices     *EmailService
}

// NewUserService creates a UserService backed by the given user store and auth config.
func NewUserService(s *store.UserStore, cfg config.AuthConfig) *UserService {
	return &UserService{store: s, cfg: cfg, noreplyHost: defaultNoreplyHost, reauth: NewReauthService(s, NewTOTPService(s))}
}

func (s *UserService) WithNoreplyHostFrom(baseURL string) *UserService {
	s.noreplyHost = noreplyHostFromBaseURL(baseURL)
	return s
}

// WithReauth confirms email changes with r, which can email a code to accounts
// with no password or 2FA.
func (s *UserService) WithReauth(r *ReauthService) *UserService {
	s.reauth = r
	return s
}

// Without it, a password change mails no notice.
func (s *UserService) WithSecurityNotices(e *EmailService) *UserService {
	s.notices = e
	return s
}

// Without it, new and changed addresses get no verification email.
func (s *UserService) WithEmailVerification(v *EmailVerificationService) *UserService {
	s.verifier = v
	return s
}

// The address change or sign-up already succeeded, so a link that can't be
// issued now is left for the user to request from settings.
func (s *UserService) sendVerification(ctx context.Context, userID int64) {
	if s.verifier == nil {
		return
	}
	err := s.verifier.Send(ctx, userID)
	if err != nil && !errors.Is(err, ErrVerificationCooldown) && !errors.Is(err, ErrEmailVerificationUnavailable) {
		slog.Error("issue verification email", "user_id", userID, "error", err)
	}
}

func (s *UserService) Create(ctx context.Context, username, email, password string) (*model.User, error) {
	if err := ValidateOwnerName(username); err != nil {
		return nil, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	u := &model.User{
		Username:     username,
		Email:        email,
		PasswordHash: hash,
	}
	if err := s.store.Create(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

func (s *UserService) CreateSuperadmin(ctx context.Context, username, email, password string) (*model.User, error) {
	if err := ValidateOwnerName(username); err != nil {
		return nil, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	return s.store.CreateSuperadmin(ctx, username, email, hash)
}

func (s *UserService) Authenticate(ctx context.Context, email, password string) (*model.User, string, error) {
	u, err := s.store.GetByEmailWithRole(ctx, email)
	if err != nil {
		_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(password))
		return nil, "", fmt.Errorf("invalid credentials")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, "", fmt.Errorf("invalid credentials")
	}
	token, err := s.generateJWT(u)
	if err != nil {
		return nil, "", err
	}
	return u, token, nil
}

func (s *UserService) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	return s.store.GetByUsername(ctx, username)
}

// CreateFromInvitation creates the invitee's account and redeems inv atomically.
// It returns ErrInvitationUnusable if inv was redeemed, expired, or its email
// registered since it was loaded.
func (s *UserService) CreateFromInvitation(ctx context.Context, inv *model.Invitation, username, password string) (*model.User, error) {
	if err := ValidateOwnerName(username); err != nil {
		return nil, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	u := &model.User{
		Username:     username,
		Email:        inv.Email,
		PasswordHash: hash,
		IsInvited:    true,
	}
	if err := s.store.CreateFromInvitation(ctx, u, inv.ID); err != nil {
		return nil, err
	}
	// The invite link came from an admin, not from the inbox, so it proves nothing about the address.
	s.sendVerification(ctx, u.ID)
	return u, nil
}

// OAuthIdentity is what an OAuth provider asserts about the person signing in.
type OAuthIdentity struct {
	Provider      string
	ID            string
	Email         string
	EmailVerified bool
	Name          string
	AvatarURL     string
}

// OAuthLogin is who an OAuth sign-in authenticated. Callers still apply the
// account's second factor before issuing Token.
type OAuthLogin struct {
	User  *model.User
	Token string
	// Link is set when the identity matched an existing account by email. The
	// account isn't linked until OAuthLinkService.LinkByVerifiedEmail, which callers run only once
	// every other factor has passed: a link made earlier would outlast a failed one.
	Link *OAuthLink
}

// OAuthLink is an identity waiting to be linked to the account UserID.
type OAuthLink struct {
	UserID   int64
	Email    string
	Provider string
	ID       string
}

func (s *UserService) AuthenticateOAuth(ctx context.Context, id OAuthIdentity, allowRegistration, allowLogin bool) (*OAuthLogin, error) {
	if u, err := s.store.GetByOAuthID(ctx, id.Provider, id.ID); err == nil {
		return s.oauthLogin(u, allowLogin, nil)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	// An unverified provider email is only a claim; an account created on it would let anyone take the address first.
	if !id.EmailVerified {
		return nil, ErrOAuthEmailUnverified
	}

	existing, err := s.store.GetByEmailWithRole(ctx, id.Email)
	if err == nil {
		return s.linkOAuth(existing, id, allowLogin)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	if !allowRegistration {
		return nil, ErrRegistrationDisabled
	}
	username := s.uniqueUsername(ctx, id.Email, id.Name)
	u, err := s.store.CreateOAuthUser(ctx, username, id.Email, id.Provider, id.ID, id.AvatarURL, id.EmailVerified)
	if err != nil {
		return nil, err
	}
	return s.oauthLogin(u, true, nil)
}

// Both sides must have proven the address: the provider's word alone would
// hand over an account whose owner typed someone else's email, and an
// unverified local address may belong to whoever registered it first.
func (s *UserService) linkOAuth(u *model.User, id OAuthIdentity, allowLogin bool) (*OAuthLogin, error) {
	switch {
	case u.OAuthProvider != "":
		return nil, ErrOAuthAlreadyLinked
	case !u.EmailVerified():
		return nil, ErrOAuthAccountExists
	// Without a password nothing guards the account's email, so a stolen session
	// could set its own address, verify it, and pull in its own Google account.
	case u.PasswordHash == "":
		return nil, ErrOAuthAccountExists
	}
	return s.oauthLogin(u, allowLogin, &OAuthLink{UserID: u.ID, Email: u.Email, Provider: id.Provider, ID: id.ID})
}

func (s *UserService) oauthLogin(u *model.User, allowLogin bool, link *OAuthLink) (*OAuthLogin, error) {
	if !loginAllowed(u, allowLogin) {
		return nil, ErrLoginDisabled
	}
	token, err := s.generateJWT(u)
	if err != nil {
		return nil, err
	}
	return &OAuthLogin{User: u, Token: token, Link: link}, nil
}

func loginAllowed(u *model.User, allowLogin bool) bool {
	return allowLogin || u.IsSuperadmin || u.IsInvited
}

func (s *UserService) uniqueUsername(ctx context.Context, email, name string) string {
	fromName := nonAlphanumRe.ReplaceAllString(strings.ToLower(strings.ReplaceAll(name, " ", "")), "")
	local, _, _ := strings.Cut(email, "@")
	fromEmail := nonAlphanumRe.ReplaceAllString(strings.ToLower(local), "")
	return freeOwnerName(ctx, s.store, fitOwnerName(fromName, fitOwnerName(fromEmail, "user")))
}

// GetByID returns a user by their numeric ID.
func (s *UserService) GetByID(ctx context.Context, id int64) (*model.User, error) {
	return s.store.GetByID(ctx, id)
}

// Missing usernames are silently omitted; result order is undefined.
func (s *UserService) GetManyByUsernames(ctx context.Context, usernames []string) ([]model.User, error) {
	return s.store.GetManyByUsernames(ctx, usernames)
}

func (s *UserService) UpdateNotificationPrefs(ctx context.Context, userID int64, p model.NotificationPrefs) error {
	if !slices.Contains(model.EmailDigestModes, p.EmailDigest) {
		p.EmailDigest = model.EmailDigestImmediate
	}
	return s.store.UpdateNotificationPrefs(ctx, userID, p)
}

// Username is deliberately not editable: repo owner names, on-disk repo paths, and JWT claims key off it.
// A new email needs confirm: the address decides who can verify it, and so who
// can link a Google account to this one.
func (s *UserService) UpdateProfile(ctx context.Context, userID int64, name, email, bio, company, location string, confirm Confirmation) error {
	if !emailRe.MatchString(email) {
		return ErrInvalidEmail
	}
	if existing, err := s.store.GetByEmail(ctx, email); err == nil && existing.ID != userID {
		return ErrEmailTaken
	}
	current, err := s.store.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if current.Email != email {
		if _, err := s.reauth.Confirm(ctx, userID, confirm); err != nil {
			return err
		}
	}
	changed, err := s.store.UpdateProfile(ctx, userID, strings.TrimSpace(name), email, strings.TrimSpace(bio), strings.TrimSpace(company), strings.TrimSpace(location))
	if err != nil {
		return err
	}
	if changed {
		if s.verifier != nil {
			s.verifier.notifyAddressChanged(current.Username, current.Email, email)
		}
		s.sendVerification(ctx, userID)
	}
	return nil
}

// Related rows go via DB cascades; repo directories via DeleteWithOwner.
// The sole-owner check runs before any dir moves, and again under lock.
func (s *UserService) DeleteUser(ctx context.Context, userID int64) error {
	if sole, err := s.store.IsSoleOrgOwner(ctx, userID); err != nil {
		return err
	} else if sole {
		return ErrSoleOrgOwner
	}
	return s.repos.DeleteWithOwner(ctx, userID, func(livePersonalIDs []int64) error {
		err := s.store.DeleteWithOwnedRepos(ctx, userID, livePersonalIDs)
		if errors.Is(err, store.ErrLastOrgOwner) {
			return ErrSoleOrgOwner
		}
		return err
	})
}

func (s *UserService) UpdateKeepEmailPrivate(ctx context.Context, userID int64, keep bool) error {
	return s.store.UpdateKeepEmailPrivate(ctx, userID, keep)
}

func (s *UserService) NoreplyEmail(_ context.Context, u *model.User) string {
	return noreplyEmail(s.noreplyHost, u)
}

func (s *UserService) CommitAuthor(ctx context.Context, userID int64) (GitAuthor, error) {
	u, err := s.store.GetByID(ctx, userID)
	if err != nil {
		return GitAuthor{}, err
	}
	return commitAuthorFor(s.noreplyHost, u), nil
}

// ListUsersForDigest returns users with email notifications enabled for the given digest mode.
func (s *UserService) ListUsersForDigest(ctx context.Context, digestMode string) ([]model.User, error) {
	return s.store.ListUsersForDigest(ctx, digestMode)
}

// GenerateTokenForUser generates a JWT for an existing user by ID.
// Used by the TOTP verification flow after a successful 2FA check.
func (s *UserService) GenerateTokenForUser(ctx context.Context, userID int64) (string, error) {
	u, err := s.store.GetByID(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("get user: %w", err)
	}
	return s.generateJWT(u)
}

// Required by PinRepo and PinnedRepos, which apply repo visibility, and by
// DeleteUser, which removes the user's repo directories.
func (s *UserService) WithRepoService(repos *RepoService) *UserService {
	s.repos = repos
	return s
}

// PinnedRepos returns the pins viewerID can read, in pin order. Stored IDs of
// deleted repos are skipped rather than erroring, since deletion doesn't unpin.
func (s *UserService) PinnedRepos(ctx context.Context, userID int64, viewerID *int64) ([]model.Repository, error) {
	ids, err := s.store.GetPinnedRepoIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	visible, _, err := s.splitPins(ctx, ids, viewerID)
	return visible, err
}

// splitPins returns the pins viewerID can read, in pin order, and the IDs of
// the rest.
func (s *UserService) splitPins(ctx context.Context, ids []int64, viewerID *int64) ([]model.Repository, []int64, error) {
	visible := make([]model.Repository, 0, len(ids))
	var hidden []int64
	for _, id := range ids {
		repo, err := s.repos.GetByID(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			hidden = append(hidden, id)
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		if s.repos.CanRead(ctx, repo, viewerID) {
			visible = append(visible, *repo)
		} else {
			hidden = append(hidden, id)
		}
	}
	return visible, hidden, nil
}

// PinRepo is idempotent. The limit counts only pins the user can still see —
// the same set their profile shows — and pinning prunes the rest, so deleted
// or now-unreadable repos don't hold slots the UI reports as free.
func (s *UserService) PinRepo(ctx context.Context, userID, repoID int64) error {
	repo, err := s.repos.GetByID(ctx, repoID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrRepoNotFound
	}
	if err != nil {
		return err
	}
	if !s.repos.CanRead(ctx, repo, &userID) {
		return ErrRepoNotFound
	}
	ids, err := s.store.GetPinnedRepoIDs(ctx, userID)
	if err != nil {
		return err
	}
	// Checked before the store locks the row so the lock never waits on repo
	// lookups; a pin landing in between was checked by its own request.
	_, stale, err := s.splitPins(ctx, ids, &userID)
	if err != nil {
		return err
	}
	pinned, err := s.store.AddPinnedRepo(ctx, userID, repoID, stale, MaxPinnedRepos)
	if err != nil {
		return err
	}
	if !pinned {
		return ErrPinLimit
	}
	return nil
}

// UnpinRepo is a no-op when repoID is not pinned.
func (s *UserService) UnpinRepo(ctx context.Context, userID, repoID int64) error {
	return s.store.RemovePinnedRepo(ctx, userID, repoID)
}

// RevokeSessions ends every session userID has, including the caller's, and
// returns a token for a fresh one.
func (s *UserService) RevokeSessions(ctx context.Context, userID int64) (string, error) {
	if _, err := s.store.BumpSessionVersion(ctx, userID); err != nil {
		return "", err
	}
	u, err := s.store.GetByID(ctx, userID)
	if err != nil {
		return "", err
	}
	return s.generateJWT(u)
}

// ChangePassword replaces userID's password once the current one (and the
// two-factor code, when 2FA is on) confirms it, and ends every session,
// including the caller's; the returned token starts a new one.
func (s *UserService) ChangePassword(ctx context.Context, userID int64, c Confirmation, newPassword string) (string, error) {
	switch {
	case len(newPassword) < MinPasswordLen:
		return "", ErrPasswordTooShort
	case len(newPassword) > MaxPasswordBytes:
		return "", ErrPasswordTooLong
	}
	confirmed, err := s.reauth.ConfirmWithPassword(ctx, userID, c)
	if err != nil {
		return "", err
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return "", err
	}
	changed, err := s.store.ChangePassword(ctx, userID, confirmed.PasswordHash, hash)
	if err != nil {
		return "", err
	}
	// Another change landed after the check, so what was confirmed is no longer the password.
	if !changed {
		return "", ErrReauthFailed
	}
	notifySecurityChange(s.notices, s.store, userID, "password_change", passwordChangedNotice)
	u, err := s.store.GetByID(ctx, userID)
	if err != nil {
		return "", err
	}
	return s.generateJWT(u)
}

// SessionVersion is what a live session's JWT must carry; see RevokeSessions.
func (s *UserService) SessionVersion(ctx context.Context, userID int64) (int, error) {
	return s.store.SessionVersion(ctx, userID)
}

func (s *UserService) generateJWT(u *model.User) (string, error) {
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
