package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	nonAlphanumRe           = regexp.MustCompile(`[^a-z0-9_-]`)
	emailRe                 = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

const MaxPinnedRepos = 6

// MaxPasswordBytes is bcrypt's input limit. Hashing longer passwords fails with
// bcrypt.ErrPasswordTooLong, so forms check this first to give a clear message.
const MaxPasswordBytes = 72

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
}

// NewUserService creates a UserService backed by the given user store and auth config.
func NewUserService(s *store.UserStore, cfg config.AuthConfig) *UserService {
	return &UserService{store: s, cfg: cfg, noreplyHost: defaultNoreplyHost}
}

func (s *UserService) WithNoreplyHostFrom(baseURL string) *UserService {
	s.noreplyHost = noreplyHostFromBaseURL(baseURL)
	return s
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

func (s *UserService) AuthenticateOAuth(ctx context.Context, id OAuthIdentity, allowRegistration, allowLogin bool) (*model.User, string, error) {
	if u, err := s.store.GetByOAuthID(ctx, id.Provider, id.ID); err == nil {
		if !u.IsSuperadmin && !u.IsInvited && !allowLogin {
			return nil, "", ErrLoginDisabled
		}
		token, err := s.generateJWT(u)
		return u, token, err
	}

	// An unverified provider email is only a claim; an account created on it would let anyone take the address first.
	if !id.EmailVerified {
		return nil, "", ErrOAuthEmailUnverified
	}

	// Local emails are never verified, so a matching account may belong to
	// whoever typed the address first; SSO refuses to link on email too.
	if _, err := s.store.GetByEmailWithRole(ctx, id.Email); err == nil {
		return nil, "", ErrOAuthAccountExists
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, "", err
	}

	if !allowRegistration {
		return nil, "", ErrRegistrationDisabled
	}
	username := s.uniqueUsername(ctx, id.Email, id.Name)
	u, err := s.store.CreateOAuthUser(ctx, username, id.Email, id.Provider, id.ID, id.AvatarURL)
	if err != nil {
		return nil, "", err
	}
	token, err := s.generateJWT(u)
	return u, token, err
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
func (s *UserService) UpdateProfile(ctx context.Context, userID int64, name, email, bio, company, location string) error {
	if !emailRe.MatchString(email) {
		return ErrInvalidEmail
	}
	if existing, err := s.store.GetByEmail(ctx, email); err == nil && existing.ID != userID {
		return ErrEmailTaken
	}
	return s.store.UpdateProfile(ctx, userID, strings.TrimSpace(name), email, strings.TrimSpace(bio), strings.TrimSpace(company), strings.TrimSpace(location))
}

// Related rows go via DB cascades; repo directories via DeleteWithOwner.
func (s *UserService) DeleteUser(ctx context.Context, userID int64) error {
	return s.repos.DeleteWithOwner(ctx, userID, func(livePersonalIDs []int64) error {
		err := s.store.DeleteWithOwnedRepos(ctx, userID, livePersonalIDs)
		if errors.Is(err, store.ErrUserOwnsOrgRepos) {
			return ErrOwnsOrgRepos
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

func (s *UserService) generateJWT(u *model.User) (string, error) {
	claims := jwt.MapClaims{
		"sub":           u.ID,
		"username":      u.Username,
		"is_superadmin": u.IsSuperadmin,
		"exp":           time.Now().Add(s.cfg.JWTExpiry).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.cfg.JWTSecret))
}
