package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

var ErrSignupTokenUnusable = errors.New("signup link is not usable")

// A link whose email already has an account is unusable, as for invitations.
const usableSignupTokenCond = `used_at IS NULL AND expires_at > NOW()
	AND NOT EXISTS (SELECT 1 FROM users WHERE lower(users.email) = lower(signup_tokens.email))`

// SignupTokenStore provides database operations for email-first signup links.
type SignupTokenStore struct {
	db *sql.DB
}

// NewSignupTokenStore creates a SignupTokenStore backed by the given database.
func NewSignupTokenStore(db *sql.DB) *SignupTokenStore {
	return &SignupTokenStore{db: db}
}

// Issue stores a link for email, replacing any earlier one. It writes nothing
// and returns false when the address was issued a link in the last 5 minutes.
func (s *SignupTokenStore) Issue(ctx context.Context, email, tokenHash string, expiresAt time.Time) (bool, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO signup_tokens (token_hash, email, expires_at) VALUES ($1, $2, $3)
		 ON CONFLICT ((lower(email))) DO UPDATE
		    SET token_hash = EXCLUDED.token_hash, email = EXCLUDED.email,
		        created_at = NOW(), expires_at = EXCLUDED.expires_at, used_at = NULL
		  WHERE signup_tokens.created_at < NOW() - INTERVAL '5 minutes'
		 RETURNING id`,
		tokenHash, email, expiresAt,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("signup token issue: %w", err)
	}
	return true, nil
}

func (s *SignupTokenStore) GetUsableByHash(ctx context.Context, tokenHash string) (*model.SignupToken, error) {
	t := &model.SignupToken{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, email, expires_at FROM signup_tokens WHERE token_hash = $1 AND `+usableSignupTokenCond,
		tokenHash,
	).Scan(&t.ID, &t.Email, &t.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSignupTokenUnusable
	}
	if err != nil {
		return nil, fmt.Errorf("signup token get usable: %w", err)
	}
	return t, nil
}
