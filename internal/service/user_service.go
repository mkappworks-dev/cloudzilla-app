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
	ErrUsernameTaken        = store.ErrUsernameTaken
	ErrEmailTaken           = store.ErrEmailTaken
	nonAlphanumRe           = regexp.MustCompile(`[^a-z0-9_-]`)
)

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
	if u.KeepEmailPrivate || u.Email == "" {
		return GitAuthor{Name: u.Username, Email: s.NoreplyEmail(ctx, u)}, nil
	}
	return GitAuthor{Name: u.Username, Email: u.Email}, nil
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
