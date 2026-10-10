package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ClaimReauthAttempt counts an attempt against userID's failure window and
// reports false, counting nothing, once limit attempts in the window are spent.
// The row lock makes concurrent claims take turns, so at most limit get through.
func (s *UserStore) ClaimReauthAttempt(ctx context.Context, userID int64, limit int, window time.Duration) (bool, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`UPDATE users SET
		   reauth_failures = CASE WHEN reauth_window_start IS NULL OR reauth_window_start <= NOW() - make_interval(secs => $3)
		                          THEN 1 ELSE reauth_failures + 1 END,
		   reauth_window_start = CASE WHEN reauth_window_start IS NULL OR reauth_window_start <= NOW() - make_interval(secs => $3)
		                              THEN NOW() ELSE reauth_window_start END
		 WHERE id = $1
		   AND (reauth_window_start IS NULL OR reauth_window_start <= NOW() - make_interval(secs => $3) OR reauth_failures < $2)
		 RETURNING id`,
		userID, limit, window.Seconds(),
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("user claim reauth attempt: %w", err)
	}
	return true, nil
}

// ReleaseReauthAttempt uncounts a claimed attempt that succeeded, so only failures use up the window.
func (s *UserStore) ReleaseReauthAttempt(ctx context.Context, userID int64) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE users SET reauth_failures = GREATEST(reauth_failures - 1, 0) WHERE id = $1`, userID,
	); err != nil {
		return fmt.Errorf("user release reauth attempt: %w", err)
	}
	return nil
}

// IssueReauthCode stores hash as userID's emailed confirmation code for ttl,
// replacing any earlier one. It reports false, storing nothing, when a code went
// out within gap or perWindow codes went out within window; the checks and the
// write are one statement.
func (s *UserStore) IssueReauthCode(ctx context.Context, userID int64, hash string, ttl, gap time.Duration, perWindow int, window time.Duration) (bool, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`UPDATE users SET reauth_code_hash = $2,
		   reauth_code_expires_at = NOW() + make_interval(secs => $3),
		   reauth_code_sent_at = NOW(),
		   reauth_codes_sent = CASE WHEN reauth_codes_window_start IS NULL OR reauth_codes_window_start <= NOW() - make_interval(secs => $6)
		                            THEN 1 ELSE reauth_codes_sent + 1 END,
		   reauth_codes_window_start = CASE WHEN reauth_codes_window_start IS NULL OR reauth_codes_window_start <= NOW() - make_interval(secs => $6)
		                                    THEN NOW() ELSE reauth_codes_window_start END
		 WHERE id = $1
		   AND (reauth_code_sent_at IS NULL OR reauth_code_sent_at <= NOW() - make_interval(secs => $4))
		   AND (reauth_codes_window_start IS NULL OR reauth_codes_window_start <= NOW() - make_interval(secs => $6) OR reauth_codes_sent < $5)
		 RETURNING id`,
		userID, hash, ttl.Seconds(), gap.Seconds(), perWindow, window.Seconds(),
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("user issue reauth code: %w", err)
	}
	return true, nil
}

// StoreReauthCode stores hash as userID's one-time confirmation code for ttl,
// replacing any earlier one. A fresh provider sign-in issues these, so no cap applies.
func (s *UserStore) StoreReauthCode(ctx context.Context, userID int64, hash string, ttl time.Duration) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE users SET reauth_code_hash = $2, reauth_code_expires_at = NOW() + make_interval(secs => $3) WHERE id = $1`,
		userID, hash, ttl.Seconds()); err != nil {
		return fmt.Errorf("user store reauth code: %w", err)
	}
	return nil
}

// LiveReauthCode returns the hash of userID's unexpired emailed code, or ""."
func (s *UserStore) LiveReauthCode(ctx context.Context, userID int64) (string, error) {
	var hash string
	err := s.db.QueryRowContext(ctx,
		`SELECT CASE WHEN reauth_code_expires_at > NOW() THEN reauth_code_hash ELSE '' END FROM users WHERE id = $1`,
		userID).Scan(&hash)
	if err != nil {
		return "", fmt.Errorf("user live reauth code: %w", err)
	}
	return hash, nil
}

// SpendReauthCode uses up the code with hash, reporting false when it was
// already spent, replaced or expired, so a code confirms one action.
func (s *UserStore) SpendReauthCode(ctx context.Context, userID int64, hash string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET reauth_code_hash = '', reauth_code_expires_at = NULL
		 WHERE id = $1 AND reauth_code_hash = $2 AND reauth_code_hash <> '' AND reauth_code_expires_at > NOW()`,
		userID, hash)
	if err != nil {
		return false, fmt.Errorf("user spend reauth code: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("user spend reauth code rows: %w", err)
	}
	return n == 1, nil
}
