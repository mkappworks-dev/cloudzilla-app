package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mkappworks/cloudzilla/internal/config"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
	"golang.org/x/crypto/bcrypt"
)

var nonAlphanumRe = regexp.MustCompile(`[^a-z0-9_-]`)

type UserService struct {
	store *store.UserStore
	cfg   config.AuthConfig
}

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

func (s *UserService) Authenticate(ctx context.Context, email, password string) (*model.User, string, error) {
	u, err := s.store.GetByEmail(ctx, email)
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

func (s *UserService) AuthenticateOAuth(ctx context.Context, provider, oauthID, email, name, avatarURL string) (*model.User, string, error) {
	// 1. Look up by OAuth ID
	if u, err := s.store.GetByOAuthID(ctx, provider, oauthID); err == nil {
		token, err := s.generateJWT(u)
		return u, token, err
	}

	// 2. Look up by email — link existing account
	if u, err := s.store.GetByEmail(ctx, email); err == nil {
		if err := s.store.LinkOAuth(ctx, u.ID, provider, oauthID); err != nil {
			return nil, "", err
		}
		token, err := s.generateJWT(u)
		return u, token, err
	}

	// 3. Create new user
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

func (s *UserService) generateJWT(u *model.User) (string, error) {
	claims := jwt.MapClaims{
		"sub":      u.ID,
		"username": u.Username,
		"exp":      time.Now().Add(s.cfg.JWTExpiry).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.cfg.JWTSecret))
}
