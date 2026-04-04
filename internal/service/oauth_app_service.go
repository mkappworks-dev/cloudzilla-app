package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"time"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
	"golang.org/x/crypto/bcrypt"
)

type OAuthAppService struct {
	apps  *store.OAuthAppStore
	auths *store.OAuthAuthorizationStore
}

func NewOAuthAppService(apps *store.OAuthAppStore, auths *store.OAuthAuthorizationStore) *OAuthAppService {
	return &OAuthAppService{apps: apps, auths: auths}
}

func (s *OAuthAppService) CreateApp(ctx context.Context, ownerID int64, name, homepageURL, description string, redirectURIs []string) (*model.OAuthApp, string, error) {
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

// IsRedirectURIAllowed returns true when redirectURI is in the app's allowed
// list, or when the app has no registered redirect URIs (unrestricted).
func (s *OAuthAppService) IsRedirectURIAllowed(app *model.OAuthApp, redirectURI string) bool {
	if len(app.RedirectURIs) == 0 {
		return true
	}
	return slices.Contains(app.RedirectURIs, redirectURI)
}

// Authorize creates an authorization code for the given user + app + scopes.
// It validates that redirectURI is in the app's allowed list.
func (s *OAuthAppService) Authorize(ctx context.Context, appID, userID int64, redirectURI string, scopes []string, app *model.OAuthApp) (code string, err error) {
	if len(app.RedirectURIs) > 0 && !slices.Contains(app.RedirectURIs, redirectURI) {
		return "", fmt.Errorf("redirect_uri not allowed")
	}
	codeBytes := make([]byte, 16)
	if _, err := rand.Read(codeBytes); err != nil {
		return "", fmt.Errorf("generate code: %w", err)
	}
	codeHex := hex.EncodeToString(codeBytes)
	expiresAt := time.Now().Add(5 * time.Minute)
	if _, err := s.auths.Upsert(ctx, appID, userID, codeHex, expiresAt, scopes); err != nil {
		return "", err
	}
	return codeHex, nil
}

// ExchangeCode validates the authorization code and returns a raw bearer token.
func (s *OAuthAppService) ExchangeCode(ctx context.Context, clientID, clientSecret, code string) (token string, err error) {
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
	if _, err := s.auths.ExchangeCode(ctx, code, tokenHash); err != nil {
		return "", err
	}
	return rawToken, nil
}

// ResolveOAuthUserID resolves a raw OAuth bearer token to a user ID.
// Implements middleware.OAuthUserIDResolver.
func (s *OAuthAppService) ResolveOAuthUserID(ctx context.Context, rawToken string) (int64, error) {
	a, err := s.auths.GetByTokenHash(ctx, sha256HexOf(rawToken))
	if err != nil {
		return 0, err
	}
	return a.UserID, nil
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
