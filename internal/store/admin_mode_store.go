package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// AdminModeStore provides database operations for superadmin admin-mode sessions.
type AdminModeStore struct{ db *sql.DB }

func NewAdminModeStore(db *sql.DB) *AdminModeStore { return &AdminModeStore{db: db} }

func (s *AdminModeStore) Create(ctx context.Context, userID int64, tokenHash string, sessionVersion int, expiresAt time.Time) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO admin_modes (user_id, token_hash, session_version, expires_at) VALUES ($1, $2, $3, $4)`,
		userID, tokenHash, sessionVersion, expiresAt); err != nil {
		return fmt.Errorf("admin mode create: %w", err)
	}
	return nil
}

// Live returns when the admin mode for tokenHash ends, if it is still running for a
// superadmin who is not suspended and whose session is the one it was entered from.
func (s *AdminModeStore) Live(ctx context.Context, tokenHash string, userID int64, sessionVersion int, now time.Time) (time.Time, bool, error) {
	var expiresAt time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT m.expires_at FROM admin_modes m JOIN users u ON u.id = m.user_id
		 WHERE m.token_hash = $1 AND m.user_id = $2 AND m.session_version = $3
		   AND m.ended_at IS NULL AND m.expires_at > $4
		   AND u.is_superadmin AND u.suspended_at IS NULL`,
		tokenHash, userID, sessionVersion, now).Scan(&expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("admin mode live: %w", err)
	}
	return expiresAt, true, nil
}

// End stamps userID's admin mode for tokenHash as over. ended is false when
// there was nothing running to end, so a repeat records nothing twice.
func (s *AdminModeStore) End(ctx context.Context, tokenHash string, userID int64, now time.Time) (expiresAt time.Time, ended bool, err error) {
	err = s.db.QueryRowContext(ctx,
		`UPDATE admin_modes SET ended_at = $3
		 WHERE token_hash = $1 AND user_id = $2 AND ended_at IS NULL RETURNING expires_at`,
		tokenHash, userID, now).Scan(&expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("admin mode end: %w", err)
	}
	return expiresAt, true, nil
}
