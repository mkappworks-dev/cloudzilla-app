package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// GetByIDWithTOTP fetches a user by ID including TOTP columns.
func (s *UserStore) GetByIDWithTOTP(ctx context.Context, id int64) (*model.User, error) {
	u, err := s.queryUserWithTOTP(ctx, `WHERE id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("user get by id with totp: %w", err)
	}
	return u, nil
}

// GetByEmailWithTOTP fetches a user by email including TOTP fields.
func (s *UserStore) GetByEmailWithTOTP(ctx context.Context, email string) (*model.User, error) {
	u, err := s.queryUserWithTOTP(ctx, `WHERE lower(email) = lower($1) AND `+notGhost, email)
	if err != nil {
		return nil, fmt.Errorf("user get by email with totp: %w", err)
	}
	return u, nil
}

func (s *UserStore) GetByUsernameWithTOTP(ctx context.Context, username string) (*model.User, error) {
	u, err := s.queryUserWithTOTP(ctx, `WHERE username = $1 AND `+notGhost, username)
	if err != nil {
		return nil, fmt.Errorf("user get by username with totp: %w", err)
	}
	return u, nil
}

func (s *UserStore) queryUserWithTOTP(ctx context.Context, filter string, args ...any) (*model.User, error) {
	u := &model.User{}
	var backupCodesJSON sql.NullString
	// Not totp_backup_codes::text: Postgres quotes array elements only when they need it.
	err := scanUser(s.db.QueryRowContext(ctx,
		`SELECT `+userColumns+`, totp_secret, totp_enabled, array_to_json(totp_backup_codes)::text FROM users `+filter, args...),
		u, &u.TOTPSecret, &u.TOTPEnabled, &backupCodesJSON)
	if err != nil {
		return nil, err
	}
	if backupCodesJSON.Valid {
		if err := json.Unmarshal([]byte(backupCodesJSON.String), &u.TOTPBackupCodes); err != nil {
			return nil, fmt.Errorf("decode backup codes: %w", err)
		}
	}
	return u, nil
}

// SetTOTPSecret stores the base32 TOTP secret for a user (does not enable TOTP yet).
func (s *UserStore) SetTOTPSecret(ctx context.Context, userID int64, secret string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET totp_secret = $1, updated_at = NOW() WHERE id = $2`,
		secret, userID,
	)
	if err != nil {
		return fmt.Errorf("user set totp secret: %w", err)
	}
	return nil
}

// SetTOTPEnabled toggles the totp_enabled flag. Pass secret="" to clear it on disable.
func (s *UserStore) SetTOTPEnabled(ctx context.Context, userID int64, enabled bool, secret string) error {
	if enabled {
		_, err := s.db.ExecContext(ctx,
			`UPDATE users SET totp_enabled = TRUE, totp_secret = $1, updated_at = NOW() WHERE id = $2`,
			secret, userID,
		)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET totp_enabled = FALSE, totp_secret = NULL, totp_backup_codes = NULL, updated_at = NOW() WHERE id = $1`,
		userID,
	)
	return err
}

// SetBackupCodes stores bcrypt hashes of backup codes as a PostgreSQL TEXT[].
func (s *UserStore) SetBackupCodes(ctx context.Context, userID int64, codeHashes []string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET totp_backup_codes = $1, updated_at = NOW() WHERE id = $2`,
		codeHashes, userID,
	)
	if err != nil {
		return fmt.Errorf("user set backup codes: %w", err)
	}
	return nil
}
