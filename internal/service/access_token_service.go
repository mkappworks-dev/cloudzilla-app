package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// ErrScopeRequired is returned when a personal access token would be created without scopes.
var ErrScopeRequired = errors.New("at least one scope is required")

// AccessTokenService manages personal access token (PAT) generation and validation.
type AccessTokenService struct {
	tokens *store.AccessTokenStore
	users  *store.UserStore
}

// NewAccessTokenService creates an AccessTokenService backed by the given stores.
func NewAccessTokenService(tokens *store.AccessTokenStore, users *store.UserStore) *AccessTokenService {
	return &AccessTokenService{tokens: tokens, users: users}
}

// MaxAdminTokenLifetime bounds a repo:admin token, which skips confirmation
// prompts, so a leaked one stops working on its own.
const MaxAdminTokenLifetime = 90 * 24 * time.Hour

var ErrAdminTokenNoExpiry = errors.New("a repo:admin token must expire within 90 days")

// CheckNewToken validates a new token's scopes, expiry and signing key, and
// returns the scopes deduplicated and the key normalized. Callers run it before
// asking for the password, so a typo doesn't spend a confirmation attempt.
func CheckNewToken(scopes []string, expiresAt *time.Time, signingKey string) ([]string, string, error) {
	scopes, err := validateTokenScopes(scopes)
	if err != nil {
		return nil, "", err
	}
	if len(scopes) == 0 {
		return nil, "", ErrScopeRequired
	}
	admin := slices.Contains(scopes, model.ScopeRepoAdmin)
	if admin && (expiresAt == nil || expiresAt.After(time.Now().Add(MaxAdminTokenLifetime))) {
		return nil, "", ErrAdminTokenNoExpiry
	}
	if strings.TrimSpace(signingKey) == "" {
		if admin {
			return nil, "", ErrAdminTokenNeedsKey
		}
		return scopes, "", nil
	}
	key, err := parseSigningKey(signingKey)
	if err != nil {
		return nil, "", err
	}
	return scopes, key, nil
}

// validateTokenScopes is validateScopes for a personal access token, which can
// also hold repo:admin.
func validateTokenScopes(requested []string) ([]string, error) {
	var scopes []string
	for _, sc := range requested {
		if !model.IsTokenScope(sc) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidScope, sc)
		}
		if !slices.Contains(scopes, sc) {
			scopes = append(scopes, sc)
		}
	}
	return scopes, nil
}

// Generate creates a new PAT, stores only its SHA-256 hash, and returns the raw token once.
// It returns ErrScopeRequired without scopes and ErrInvalidScope for an unknown one.
func (s *AccessTokenService) Generate(ctx context.Context, userID int64, name string, scopes []string, expiresAt *time.Time) (string, *model.AccessToken, error) {
	return s.GenerateWithKey(ctx, userID, name, scopes, expiresAt, "")
}

// GenerateWithKey is Generate for a token bound to signingKey, an SSH public
// key: every request with it must be signed (see VerifySignedRequest). A
// repo:admin token must be bound, so the token string alone can't be used.
func (s *AccessTokenService) GenerateWithKey(ctx context.Context, userID int64, name string, scopes []string, expiresAt *time.Time, signingKey string) (string, *model.AccessToken, error) {
	scopes, signingKey, err := CheckNewToken(scopes, expiresAt, signingKey)
	if err != nil {
		return "", nil, err
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate token bytes: %w", err)
	}
	rawHex := "czp_" + hex.EncodeToString(raw)

	sum := sha256.Sum256([]byte(rawHex))
	hash := hex.EncodeToString(sum[:])

	t := &model.AccessToken{
		UserID:     userID,
		Name:       name,
		TokenHash:  hash,
		LastEight:  rawHex[len(rawHex)-8:],
		Scopes:     scopes,
		ExpiresAt:  expiresAt,
		SigningKey: signingKey,
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
