package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
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
	ErrPinLimit             = errors.New("pin limit reached (6)")
	ErrUsernameTaken        = errors.New("username is already taken")
	ErrInvalidUsername      = errors.New("username must be 1-39 chars, alphanumeric, dash or underscore")
	ErrEmailTaken           = errors.New("email is already taken")
	ErrInvalidEmail         = errors.New("email must be a valid address")
	nonAlphanumRe           = regexp.MustCompile(`[^a-z0-9_-]`)
	usernameRe              = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,38}$`)
	emailRe                 = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

// MaxPinnedRepos is the per-user cap on pinned repositories.
const MaxPinnedRepos = 6

// UserService manages user account operations including authentication and profile updates.
type UserService struct {
	store *store.UserStore
	cfg   config.AuthConfig
}

// NewUserService creates a UserService backed by the given user store and auth config.
func NewUserService(s *store.UserStore, cfg config.AuthConfig) *UserService {
	return &UserService{store: s, cfg: cfg}
}

func (s *UserService) Create(ctx context.Context, username, email, password string) (*model.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	u := &model.User{
		Username:     username,
		Email:        email,
		PasswordHash: string(hash),
	}
	if err := s.store.Create(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

func (s *UserService) CreateSuperadmin(ctx context.Context, username, email, password string) (*model.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	return s.store.CreateSuperadmin(ctx, username, email, string(hash))
}

func (s *UserService) Authenticate(ctx context.Context, email, password string) (*model.User, string, error) {
	u, err := s.store.GetByEmailWithRole(ctx, email)
	if err != nil {
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

func (s *UserService) MarkInvited(ctx context.Context, userID int64) error {
	return s.store.MarkInvited(ctx, userID)
}

func (s *UserService) AuthenticateOAuth(ctx context.Context, provider, oauthID, email, name, avatarURL string, allowRegistration, allowLogin bool) (*model.User, string, error) {
	// 1. Look up by OAuth ID
	if u, err := s.store.GetByOAuthID(ctx, provider, oauthID); err == nil {
		if !u.IsSuperadmin && !u.IsInvited && !allowLogin {
			return nil, "", ErrLoginDisabled
		}
		token, err := s.generateJWT(u)
		return u, token, err
	}

	// 2. Look up by email — link existing account
	if u, err := s.store.GetByEmailWithRole(ctx, email); err == nil {
		if !u.IsSuperadmin && !u.IsInvited && !allowLogin {
			return nil, "", ErrLoginDisabled
		}
		if err := s.store.LinkOAuth(ctx, u.ID, provider, oauthID); err != nil {
			return nil, "", err
		}
		token, err := s.generateJWT(u)
		return u, token, err
	}

	// 3. Create new user
	if !allowRegistration {
		return nil, "", ErrRegistrationDisabled
	}
	username := s.uniqueUsername(ctx, email, name)
	u, err := s.store.CreateOAuthUser(ctx, username, email, provider, oauthID, avatarURL)
	if err != nil {
		return nil, "", err
	}
	token, err := s.generateJWT(u)
	return u, token, err
}

func (s *UserService) uniqueUsername(ctx context.Context, email, name string) string {
	base := nonAlphanumRe.ReplaceAllString(strings.ToLower(strings.ReplaceAll(name, " ", "")), "")
	if base == "" {
		parts := strings.SplitN(email, "@", 2)
		base = nonAlphanumRe.ReplaceAllString(strings.ToLower(parts[0]), "")
	}
	if base == "" {
		base = "user"
	}
	candidate := base
	for i := 2; ; i++ {
		if _, err := s.store.GetByUsername(ctx, candidate); err != nil {
			return candidate
		}
		candidate = fmt.Sprintf("%s%d", base, i)
	}
}

// GetByID returns a user by their numeric ID.
func (s *UserService) GetByID(ctx context.Context, id int64) (*model.User, error) {
	return s.store.GetByID(ctx, id)
}

// Missing usernames are silently omitted; result order is undefined.
func (s *UserService) GetManyByUsernames(ctx context.Context, usernames []string) ([]model.User, error) {
	return s.store.GetManyByUsernames(ctx, usernames)
}

// UpdateEmailPrefs saves the user's email notification preferences.
func (s *UserService) UpdateEmailPrefs(ctx context.Context, userID int64, emailNotifications bool, emailDigest string) error {
	return s.store.UpdateEmailPrefs(ctx, userID, emailNotifications, emailDigest)
}

// UpdateNotificationPrefs saves the granular notification toggles.
func (s *UserService) UpdateNotificationPrefs(ctx context.Context, userID int64, p store.NotificationPrefs) error {
	return s.store.UpdateNotificationPrefs(ctx, userID, p)
}

// UpdateProfile validates and saves profile fields. Changing username or email
// is checked against syntax + uniqueness; other fields are stored as-is.
func (s *UserService) UpdateProfile(ctx context.Context, userID int64, name, username, email, bio, company, location string) error {
	if !usernameRe.MatchString(username) {
		return ErrInvalidUsername
	}
	if existing, err := s.store.GetByUsername(ctx, username); err == nil && existing.ID != userID {
		return ErrUsernameTaken
	}
	if !emailRe.MatchString(email) {
		return ErrInvalidEmail
	}
	if existing, err := s.store.GetByEmail(ctx, email); err == nil && existing.ID != userID {
		return ErrEmailTaken
	}
	return s.store.UpdateProfile(ctx, userID, strings.TrimSpace(name), username, email, strings.TrimSpace(bio), strings.TrimSpace(company), strings.TrimSpace(location))
}

// DeleteUser removes the user account. Related rows are removed via DB cascades.
func (s *UserService) DeleteUser(ctx context.Context, userID int64) error {
	return s.store.DeleteByID(ctx, userID)
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

// PinnedRepoIDs returns the user's pinned repo IDs in pin order.
func (s *UserService) PinnedRepoIDs(ctx context.Context, userID int64) ([]int64, error) {
	return s.store.GetPinnedRepoIDs(ctx, userID)
}

// PinRepo appends repoID to the user's pinned list. It is idempotent (pinning
// an already-pinned repo is a no-op) and returns ErrPinLimit if the user
// already has MaxPinnedRepos distinct pins.
func (s *UserService) PinRepo(ctx context.Context, userID, repoID int64) error {
	ids, err := s.store.GetPinnedRepoIDs(ctx, userID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if id == repoID {
			return nil
		}
	}
	if len(ids) >= MaxPinnedRepos {
		return ErrPinLimit
	}
	ids = append(ids, repoID)
	return s.store.SetPinnedRepoIDs(ctx, userID, ids)
}

// UnpinRepo removes repoID from the user's pinned list. Removing a repo that
// is not pinned is a no-op.
func (s *UserService) UnpinRepo(ctx context.Context, userID, repoID int64) error {
	ids, err := s.store.GetPinnedRepoIDs(ctx, userID)
	if err != nil {
		return err
	}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id != repoID {
			out = append(out, id)
		}
	}
	if len(out) == len(ids) {
		return nil
	}
	return s.store.SetPinnedRepoIDs(ctx, userID, out)
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
