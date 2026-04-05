package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/mkappworks/cloudzilla/internal/model"
)

// AccessTokenStore provides database operations for personal access tokens.
type AccessTokenStore struct{ db *sql.DB }

// NewAccessTokenStore creates an AccessTokenStore backed by the given database.
func NewAccessTokenStore(db *sql.DB) *AccessTokenStore { return &AccessTokenStore{db: db} }

func (s *AccessTokenStore) Create(ctx context.Context, t *model.AccessToken) error {
	t.ScopesRaw = strings.Join(t.Scopes, ",")
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO access_tokens (user_id, name, token_hash, last_eight, scopes, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		t.UserID, t.Name, t.TokenHash, t.LastEight, t.ScopesRaw, t.ExpiresAt,
	).Scan(&t.ID, &t.CreatedAt)
	if err != nil {
		return fmt.Errorf("access token create: %w", err)
	}
	return nil
}

func (s *AccessTokenStore) ListByUser(ctx context.Context, userID int64) ([]model.AccessToken, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, name, token_hash, last_eight, scopes, last_used_at, expires_at, created_at
		 FROM access_tokens WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("access token list by user: %w", err)
	}
	defer rows.Close()
	var tokens []model.AccessToken
	for rows.Next() {
		var t model.AccessToken
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.TokenHash, &t.LastEight, &t.ScopesRaw,
			&t.LastUsedAt, &t.ExpiresAt, &t.CreatedAt); err != nil {
			return nil, err
		}
		if t.ScopesRaw != "" {
			t.Scopes = strings.Split(t.ScopesRaw, ",")
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

func (s *AccessTokenStore) GetByHash(ctx context.Context, tokenHash string) (*model.AccessToken, error) {
	t := &model.AccessToken{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, name, token_hash, last_eight, scopes, last_used_at, expires_at, created_at
		 FROM access_tokens WHERE token_hash = $1`,
		tokenHash,
	).Scan(&t.ID, &t.UserID, &t.Name, &t.TokenHash, &t.LastEight, &t.ScopesRaw,
		&t.LastUsedAt, &t.ExpiresAt, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("access token get by hash: %w", err)
	}
	if t.ScopesRaw != "" {
		t.Scopes = strings.Split(t.ScopesRaw, ",")
	}
	return t, nil
}

func (s *AccessTokenStore) UpdateLastUsed(ctx context.Context, tokenID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE access_tokens SET last_used_at = NOW() WHERE id = $1`,
		tokenID,
	)
	return err
}

func (s *AccessTokenStore) Delete(ctx context.Context, tokenID, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM access_tokens WHERE id = $1 AND user_id = $2`,
		tokenID, userID,
	)
	return err
}
