package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// OAuthAppService manages OAuth 2.0 application registration and authorization code flow.
type OAuthAppService struct {
	apps  *store.OAuthAppStore
	auths *store.OAuthAuthorizationStore
	users *store.UserStore
}

// ErrInvalidScope is returned when an authorization request names a scope that cannot be granted.
var ErrInvalidScope = errors.New("invalid scope")

// ErrInvalidRedirectURI is returned when an app is registered without a usable redirect URI.
var ErrInvalidRedirectURI = errors.New("invalid redirect_uri")

// NewOAuthAppService creates an OAuthAppService backed by the given stores.
func NewOAuthAppService(apps *store.OAuthAppStore, auths *store.OAuthAuthorizationStore, users *store.UserStore) *OAuthAppService {
	return &OAuthAppService{apps: apps, auths: auths, users: users}
}

func (s *OAuthAppService) CreateApp(ctx context.Context, ownerID int64, name, homepageURL, description string, redirectURIs []string) (*model.OAuthApp, string, error) {
	if err := validateRedirectURIs(redirectURIs); err != nil {
		return nil, "", err
	}
	clientIDBytes := make([]byte, 10)
	if _, err := rand.Read(clientIDBytes); err != nil {
		return nil, "", fmt.Errorf("generate client_id: %w", err)
	}
	clientID := hex.EncodeToString(clientIDBytes)

	rawSecret := make([]byte, 20)
	if _, err := rand.Read(rawSecret); err != nil {
		return nil, "", fmt.Errorf("generate client_secret: %w", err)
	}
	rawSecretHex := hex.EncodeToString(rawSecret)

	hash, err := bcrypt.GenerateFromPassword([]byte(rawSecretHex), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", fmt.Errorf("hash client_secret: %w", err)
	}

	app := &model.OAuthApp{
		OwnerID:      ownerID,
		Name:         name,
		ClientID:     clientID,
		ClientSecret: string(hash),
		RedirectURIs: redirectURIs,
		HomepageURL:  homepageURL,
		Description:  description,
	}
	if err := s.apps.Create(ctx, app); err != nil {
		return nil, "", err
	}
	return app, rawSecretHex, nil
}

func (s *OAuthAppService) GetByClientID(ctx context.Context, clientID string) (*model.OAuthApp, error) {
	return s.apps.GetByClientID(ctx, clientID)
}

func (s *OAuthAppService) ListByOwner(ctx context.Context, ownerID int64) ([]model.OAuthApp, error) {
	return s.apps.ListByOwner(ctx, ownerID)
}

func (s *OAuthAppService) DeleteApp(ctx context.Context, id, ownerID int64) error {
	return s.apps.Delete(ctx, id, ownerID)
}

func validateRedirectURIs(uris []string) error {
	if len(uris) == 0 {
		return fmt.Errorf("%w: at least one is required", ErrInvalidRedirectURI)
	}
	for _, raw := range uris {
		u, err := url.Parse(raw)
		// RFC 6749 §3.1.2 bars a fragment; a comma would split the stored comma-joined list.
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || strings.ContainsAny(raw, "#,") {
			return fmt.Errorf("%w: %q", ErrInvalidRedirectURI, raw)
		}
	}
	return nil
}

// IsRedirectURIAllowed reports whether redirectURI exactly matches one the app
// registered; an app with none registered allows none.
func (s *OAuthAppService) IsRedirectURIAllowed(app *model.OAuthApp, redirectURI string) bool {
	return redirectURI != "" && slices.Contains(app.RedirectURIs, redirectURI)
}

// ParseScopes splits a space-delimited OAuth scope parameter, dropping duplicates.
// It returns ErrInvalidScope if any scope is unknown.
func (s *OAuthAppService) ParseScopes(_ context.Context, param string) ([]string, error) {
	return validateScopes(strings.Fields(param))
}

func validateScopes(requested []string) ([]string, error) {
	var scopes []string
	for _, sc := range requested {
		if !model.IsKnownScope(sc) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidScope, sc)
		}
		if !slices.Contains(scopes, sc) {
			scopes = append(scopes, sc)
		}
	}
	return scopes, nil
}

// Authorize creates an authorization code for the given user + app + scopes.
// It validates that redirectURI is in the app's allowed list and that every scope is known.
func (s *OAuthAppService) Authorize(ctx context.Context, appID, userID int64, redirectURI string, scopes []string, app *model.OAuthApp) (code string, err error) {
	if !s.IsRedirectURIAllowed(app, redirectURI) {
		return "", fmt.Errorf("redirect_uri not allowed")
	}
	if scopes, err = validateScopes(scopes); err != nil {
		return "", err
	}
	codeBytes := make([]byte, 16)
	if _, err := rand.Read(codeBytes); err != nil {
		return "", fmt.Errorf("generate code: %w", err)
	}
	codeHex := hex.EncodeToString(codeBytes)
	expiresAt := time.Now().Add(5 * time.Minute)
	if _, err := s.auths.Upsert(ctx, appID, userID, codeHex, redirectURI, expiresAt, scopes); err != nil {
		return "", err
	}
	return codeHex, nil
}

// ExchangeCode redeems a code issued to clientID for redirectURI and returns a raw bearer token.
func (s *OAuthAppService) ExchangeCode(ctx context.Context, clientID, clientSecret, code, redirectURI string) (token string, err error) {
	app, err := s.apps.GetByClientID(ctx, clientID)
	if err != nil {
		return "", fmt.Errorf("app not found")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(app.ClientSecret), []byte(clientSecret)); err != nil {
		return "", fmt.Errorf("invalid client_secret")
	}
	tokenBytes := make([]byte, 20)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	rawToken := hex.EncodeToString(tokenBytes)
	tokenHash := sha256HexOf(rawToken)
	if _, err := s.auths.ExchangeCode(ctx, app.ID, code, redirectURI, tokenHash); err != nil {
		return "", err
	}
	return rawToken, nil
}

// ResolveOAuthToken resolves a raw OAuth bearer token to its user and granted scopes.
// Implements middleware.OAuthTokenResolver.
func (s *OAuthAppService) ResolveOAuthToken(ctx context.Context, rawToken string) (*model.User, []string, error) {
	a, err := s.auths.GetByTokenHash(ctx, sha256HexOf(rawToken))
	if err != nil {
		return nil, nil, err
	}
	user, err := s.users.GetByID(ctx, a.UserID)
	if err != nil {
		return nil, nil, fmt.Errorf("oauth token user lookup: %w", err)
	}
	return user, a.Scopes, nil
}

func (s *OAuthAppService) RevokeAccess(ctx context.Context, authID, userID int64) error {
	auths, err := s.auths.ListByUser(ctx, userID)
	if err != nil {
		return err
	}
	for _, a := range auths {
		if a.ID == authID {
			return s.auths.RevokeByID(ctx, authID)
		}
	}
	return fmt.Errorf("authorization not found or not owned by user")
}

func (s *OAuthAppService) ListAuthorizationsByUser(ctx context.Context, userID int64) ([]model.OAuthAuthorization, error) {
	return s.auths.ListByUser(ctx, userID)
}

func sha256HexOf(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
