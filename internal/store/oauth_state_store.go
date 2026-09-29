package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// OAuthStateStore keeps the server side of OAuth flows that act on a signed-in account.
type OAuthStateStore struct{ db *sql.DB }

// NewOAuthStateStore creates an OAuthStateStore backed by the given database.
func NewOAuthStateStore(db *sql.DB) *OAuthStateStore {
	return &OAuthStateStore{db: db}
}

// Put makes stateHash userID's only pending state for purpose.
func (s *OAuthStateStore) Put(ctx context.Context, userID int64, purpose, stateHash string, expiresAt time.Time) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM oauth_states WHERE expires_at <= NOW()`); err != nil {
		return fmt.Errorf("oauth_state purge: %w", err)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO oauth_states (state_hash, user_id, purpose, expires_at)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (user_id, purpose) DO UPDATE
		     SET state_hash = EXCLUDED.state_hash, expires_at = EXCLUDED.expires_at, created_at = NOW()`,
		stateHash, userID, purpose, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("oauth_state put: %w", err)
	}
	return nil
}

// Take deletes the state and returns the user it was issued to. An expired state
// is deleted too, and reported as sql.ErrNoRows like one that never existed.
func (s *OAuthStateStore) Take(ctx context.Context, stateHash, purpose string) (int64, error) {
	var userID int64
	var live bool
	err := s.db.QueryRowContext(ctx,
		`DELETE FROM oauth_states WHERE state_hash = $1 AND purpose = $2
		 RETURNING user_id, expires_at > NOW()`,
		stateHash, purpose,
	).Scan(&userID, &live)
	if err == nil && !live {
		err = sql.ErrNoRows
	}
	if err != nil {
		return 0, fmt.Errorf("oauth_state take: %w", err)
	}
	return userID, nil
}
