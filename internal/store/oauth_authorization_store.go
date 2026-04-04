package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/mkappworks/cloudzilla/internal/model"
)

type OAuthAuthorizationStore struct{ db *sql.DB }

func NewOAuthAuthorizationStore(db *sql.DB) *OAuthAuthorizationStore {
	return &OAuthAuthorizationStore{db: db}
}

// Upsert creates or updates an authorization record, setting a fresh authorization code.
func (s *OAuthAuthorizationStore) Upsert(ctx context.Context, appID, userID int64, code string, expiresAt time.Time, scopes []string) (*model.OAuthAuthorization, error) {
	auth := &model.OAuthAuthorization{}
	scopesRaw := strings.Join(scopes, ",")
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO oauth_authorizations (app_id, user_id, code, scopes, code_expires_at)
         VALUES ($1, $2, $3, $4, $5)
         ON CONFLICT (app_id, user_id) DO UPDATE
             SET code = EXCLUDED.code, scopes = EXCLUDED.scopes, code_expires_at = EXCLUDED.code_expires_at, token_hash = NULL
         RETURNING id, app_id, user_id, code, token_hash, scopes, code_expires_at, created_at`,
		appID, userID, code, scopesRaw, expiresAt,
	).Scan(&auth.ID, &auth.AppID, &auth.UserID, &auth.Code, &auth.TokenHash, &auth.ScopesRaw, &auth.CodeExpiresAt, &auth.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("oauth_authorization upsert: %w", err)
	}
	if auth.ScopesRaw != "" {
		auth.Scopes = strings.Split(auth.ScopesRaw, ",")
	}
	return auth, nil
}

// ExchangeCode validates the code, clears it, and sets the token_hash.
func (s *OAuthAuthorizationStore) ExchangeCode(ctx context.Context, code, tokenHash string) (*model.OAuthAuthorization, error) {
	auth := &model.OAuthAuthorization{}
	err := s.db.QueryRowContext(ctx,
		`UPDATE oauth_authorizations
         SET code = NULL, code_expires_at = NULL, token_hash = $1
         WHERE code = $2 AND code_expires_at > NOW()
         RETURNING id, app_id, user_id, code, token_hash, scopes, code_expires_at, created_at`,
		tokenHash, code,
	).Scan(&auth.ID, &auth.AppID, &auth.UserID, &auth.Code, &auth.TokenHash, &auth.ScopesRaw, &auth.CodeExpiresAt, &auth.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("invalid or expired authorization code")
		}
		return nil, fmt.Errorf("oauth_authorization exchange: %w", err)
	}
	if auth.ScopesRaw != "" {
		auth.Scopes = strings.Split(auth.ScopesRaw, ",")
	}
	return auth, nil
}

// GetByTokenHash looks up an active authorization by its hashed bearer token.
func (s *OAuthAuthorizationStore) GetByTokenHash(ctx context.Context, hash string) (*model.OAuthAuthorization, error) {
	auth := &model.OAuthAuthorization{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, app_id, user_id, code, token_hash, scopes, code_expires_at, created_at
         FROM oauth_authorizations WHERE token_hash = $1`,
		hash,
	).Scan(&auth.ID, &auth.AppID, &auth.UserID, &auth.Code, &auth.TokenHash, &auth.ScopesRaw, &auth.CodeExpiresAt, &auth.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("oauth_authorization get by token_hash: %w", err)
	}
	if auth.ScopesRaw != "" {
		auth.Scopes = strings.Split(auth.ScopesRaw, ",")
	}
	return auth, nil
}

// RevokeByID deletes the authorization entirely.
func (s *OAuthAuthorizationStore) RevokeByID(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM oauth_authorizations WHERE id = $1`, id)
	return err
}

// ListByUser returns all authorizations belonging to a user (for the settings revoke UI).
func (s *OAuthAuthorizationStore) ListByUser(ctx context.Context, userID int64) ([]model.OAuthAuthorization, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, app_id, user_id, code, token_hash, scopes, code_expires_at, created_at
         FROM oauth_authorizations WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("oauth_authorization list by user: %w", err)
	}
	defer rows.Close()
	var auths []model.OAuthAuthorization
	for rows.Next() {
		var a model.OAuthAuthorization
		if err := rows.Scan(&a.ID, &a.AppID, &a.UserID, &a.Code, &a.TokenHash, &a.ScopesRaw, &a.CodeExpiresAt, &a.CreatedAt); err != nil {
			return nil, err
		}
		if a.ScopesRaw != "" {
			a.Scopes = strings.Split(a.ScopesRaw, ",")
		}
		auths = append(auths, a)
	}
	return auths, rows.Err()
}
