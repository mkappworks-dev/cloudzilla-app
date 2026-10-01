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
	tokens  *store.AccessTokenStore
	users   *store.UserStore
	targets *adminTargets
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
// key: every request with it must be signed (see VerifySignedRequest).
func (s *AccessTokenService) GenerateWithKey(ctx context.Context, userID int64, name string, scopes []string, expiresAt *time.Time, signingKey string) (string, *model.AccessToken, error) {
	return s.Create(ctx, userID, NewToken{Name: name, Scopes: scopes, ExpiresAt: expiresAt, SigningKey: signingKey})
}

// NewToken is what a personal access token is created with.
type NewToken struct {
	Name       string
	Scopes     []string
	ExpiresAt  *time.Time
	SigningKey string
	// Targets limit a repo:admin token to these repositories ("owner/repo") and
	// organizations ("org"), and are required for one.
	Targets []string
}

// WithAdminTargets lets repo:admin tokens be created; without it there's no
// way to check the repositories and organizations they name.
func (s *AccessTokenService) WithAdminTargets(repos *RepoService, orgs *OrgService) *AccessTokenService {
	s.targets = &adminTargets{repos: repos, orgs: orgs}
	return s
}

// Check validates t for userID and returns it normalized: scopes deduplicated,
// the key in authorized_keys form, targets canonical. Callers run it before
// asking for the password, so a typo doesn't spend a confirmation attempt.
func (s *AccessTokenService) Check(ctx context.Context, userID int64, t NewToken) (NewToken, error) {
	scopes, key, err := CheckNewToken(t.Scopes, t.ExpiresAt, t.SigningKey)
	if err != nil {
		return NewToken{}, err
	}
	t.Scopes, t.SigningKey = scopes, key
	var targets []string
	for _, target := range t.Targets {
		if strings.TrimSpace(target) != "" {
			targets = append(targets, target)
		}
	}
	if !slices.Contains(scopes, model.ScopeRepoAdmin) {
		if len(targets) > 0 {
			return NewToken{}, ErrTokenTarget
		}
		t.Targets = nil
		return t, nil
	}
	if len(targets) == 0 || s.targets == nil {
		return NewToken{}, ErrAdminTokenNeedsTargets
	}
	if len(targets) > maxTokenTargets {
		return NewToken{}, fmt.Errorf("%w: at most %d", ErrTokenTarget, maxTokenTargets)
	}
	t.Targets = nil
	for _, target := range targets {
		c, err := s.targets.canonical(ctx, userID, target)
		if err != nil {
			return NewToken{}, err
		}
		if !slices.Contains(t.Targets, c) {
			t.Targets = append(t.Targets, c)
		}
	}
	return t, nil
}

// Create checks nt (see Check) and creates the token, returning the raw token once.
func (s *AccessTokenService) Create(ctx context.Context, userID int64, nt NewToken) (string, *model.AccessToken, error) {
	nt, err := s.Check(ctx, userID, nt)
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
		Name:       nt.Name,
		TokenHash:  hash,
		LastEight:  rawHex[len(rawHex)-8:],
		Scopes:     nt.Scopes,
		ExpiresAt:  nt.ExpiresAt,
		SigningKey: nt.SigningKey,
		Targets:    nt.Targets,
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
