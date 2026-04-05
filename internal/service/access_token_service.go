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

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

// AccessTokenService manages personal access token (PAT) generation and validation.
type AccessTokenService struct {
	tokens *store.AccessTokenStore
	users  *store.UserStore
}

// NewAccessTokenService creates an AccessTokenService backed by the given stores.
func NewAccessTokenService(tokens *store.AccessTokenStore, users *store.UserStore) *AccessTokenService {
	return &AccessTokenService{tokens: tokens, users: users}
}

// Generate creates a new PAT, stores only its SHA-256 hash, and returns the raw token once.
func (s *AccessTokenService) Generate(ctx context.Context, userID int64, name string, scopes []string, expiresAt *time.Time) (string, *model.AccessToken, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate token bytes: %w", err)
	}
	rawHex := "czp_" + hex.EncodeToString(raw)

	sum := sha256.Sum256([]byte(rawHex))
	hash := hex.EncodeToString(sum[:])

	t := &model.AccessToken{
		UserID:    userID,
		Name:      name,
		TokenHash: hash,
		LastEight: rawHex[len(rawHex)-8:],
		Scopes:    scopes,
		ExpiresAt: expiresAt,
	}
	if err := s.tokens.Create(ctx, t); err != nil {
		return "", nil, err
	}
	return rawHex, t, nil
}

// Validate checks a raw PAT and returns the token record and its owner.
func (s *AccessTokenService) Validate(ctx context.Context, rawToken string) (*model.AccessToken, *model.User, error) {
	if !strings.HasPrefix(rawToken, "czp_") {
		return nil, nil, errors.New("not a PAT")
	}
	sum := sha256.Sum256([]byte(rawToken))
	hash := hex.EncodeToString(sum[:])

	t, err := s.tokens.GetByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, errors.New("token not found")
		}
		return nil, nil, err
	}
	if t.ExpiresAt != nil && time.Now().After(*t.ExpiresAt) {
		return nil, nil, errors.New("token expired")
	}
	user, err := s.users.GetByID(ctx, t.UserID)
	if err != nil {
		return nil, nil, fmt.Errorf("token user lookup: %w", err)
	}
	return t, user, nil
}

// UpdateLastUsed updates last_used_at for a token. Intended to be called asynchronously.
func (s *AccessTokenService) UpdateLastUsed(ctx context.Context, tokenID int64) error {
	return s.tokens.UpdateLastUsed(ctx, tokenID)
}

func (s *AccessTokenService) List(ctx context.Context, userID int64) ([]model.AccessToken, error) {
	return s.tokens.ListByUser(ctx, userID)
}

func (s *AccessTokenService) Delete(ctx context.Context, tokenID, userID int64) error {
	return s.tokens.Delete(ctx, tokenID, userID)
}
