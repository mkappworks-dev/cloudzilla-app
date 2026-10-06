package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

var ErrPasswordResetCooldown = errors.New("a password reset email was sent too recently")

// PasswordResetStore keeps each account's newest password reset link by its
// SHA-256. Methods lock the users row first, the order EmailVerificationStore uses.
type PasswordResetStore struct {
	db *sql.DB
}

func NewPasswordResetStore(database *sql.DB) *PasswordResetStore {
	return &PasswordResetStore{db: database}
}

// Issue replaces userID's link with tokenHash, bound to their current email
// and session version. An empty tokenHash records an email that carried no
// link. A cooldown above zero refuses the issue while an emailed one is newer.
func (s *PasswordResetStore) Issue(ctx context.Context, userID int64, tokenHash, issuedBy string, ttl, cooldown time.Duration) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("password reset issue begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var email string
	var sessionVersion int
	if err := tx.QueryRowContext(ctx,
		`SELECT email, session_version FROM users WHERE id = $1 FOR UPDATE`, userID,
	).Scan(&email, &sessionVersion); err != nil {
		return fmt.Errorf("password reset issue lock user: %w", err)
	}
	if cooldown > 0 {
		var recent bool
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM password_reset_tokens
			                 WHERE user_id = $1 AND issued_by = 'email'
			                   AND created_at > NOW() - make_interval(secs => $2))`,
			userID, cooldown.Seconds(),
		).Scan(&recent); err != nil {
			return fmt.Errorf("password reset issue cooldown: %w", err)
		}
		if recent {
			return ErrPasswordResetCooldown
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO password_reset_tokens (user_id, email, token_hash, session_version, issued_by, expires_at)
		 VALUES ($1, $2, NULLIF($3, ''), $4, $5, NOW() + make_interval(secs => $6))
		 ON CONFLICT (user_id) DO UPDATE
		   SET email = EXCLUDED.email, token_hash = EXCLUDED.token_hash, session_version = EXCLUDED.session_version,
		       issued_by = EXCLUDED.issued_by, created_at = NOW(), expires_at = EXCLUDED.expires_at, used_at = NULL`,
		userID, email, tokenHash, sessionVersion, issuedBy, ttl.Seconds(),
	); err != nil {
		return fmt.Errorf("password reset issue upsert: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("password reset issue commit: %w", err)
	}
	return nil
}

// Every condition but expiry makes a link invalid rather than expired, so it
// never says which of them failed.
const passwordResetUsable = `t.used_at IS NULL AND t.email = u.email
	AND t.session_version = u.session_version AND u.password_hash <> ''`

// Lookup reports what spending tokenHash now would do, without spending it.
func (s *PasswordResetStore) Lookup(ctx context.Context, tokenHash string) (model.PasswordResetLink, error) {
	var usable, expired bool
	var link model.PasswordResetLink
	err := s.db.QueryRowContext(ctx,
		`SELECT `+passwordResetUsable+`, t.expires_at <= NOW(), u.id, u.username, u.email, t.issued_by, u.totp_enabled
		   FROM password_reset_tokens t JOIN users u ON u.id = t.user_id
		  WHERE t.token_hash = $1`,
		tokenHash,
	).Scan(&usable, &expired, &link.UserID, &link.Username, &link.Email, &link.IssuedBy, &link.TOTPEnabled)
	if errors.Is(err, sql.ErrNoRows) {
		return model.PasswordResetLink{State: model.PasswordResetInvalid}, nil
	}
	if err != nil {
		return model.PasswordResetLink{}, fmt.Errorf("password reset lookup: %w", err)
	}
	switch {
	case !usable:
		return model.PasswordResetLink{State: model.PasswordResetInvalid}, nil
	case expired:
		return model.PasswordResetLink{State: model.PasswordResetExpired}, nil
	}
	link.State = model.PasswordResetPending
	return link, nil
}

// Consume spends tokenHash: it sets passwordHash, ends every session, and
// verifies the address the link was sent to. The user and who issued the link
// are returned only when the result is PasswordResetDone.
func (s *PasswordResetStore) Consume(ctx context.Context, tokenHash, passwordHash string) (state model.PasswordResetState, u *model.User, issuedBy string, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", nil, "", fmt.Errorf("password reset consume begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var userID int64
	err = tx.QueryRowContext(ctx, `SELECT user_id FROM password_reset_tokens WHERE token_hash = $1`, tokenHash).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return model.PasswordResetInvalid, nil, "", nil
	}
	if err != nil {
		return "", nil, "", fmt.Errorf("password reset consume find: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, userID); err != nil {
		return "", nil, "", fmt.Errorf("password reset consume lock user: %w", err)
	}
	// Re-read under lock: a concurrent reset, issue or password change may have
	// spent or replaced the link since the first read.
	var usable, expired bool
	err = tx.QueryRowContext(ctx,
		`SELECT `+passwordResetUsable+`, t.expires_at <= NOW(), t.issued_by
		   FROM password_reset_tokens t JOIN users u ON u.id = t.user_id
		  WHERE t.token_hash = $1 AND t.user_id = $2`,
		tokenHash, userID,
	).Scan(&usable, &expired, &issuedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return model.PasswordResetInvalid, nil, "", nil
	}
	if err != nil {
		return "", nil, "", fmt.Errorf("password reset consume recheck: %w", err)
	}
	switch {
	case !usable:
		return model.PasswordResetInvalid, nil, "", nil
	case expired:
		return model.PasswordResetExpired, nil, "", nil
	}

	if _, err := tx.ExecContext(ctx, `UPDATE password_reset_tokens SET used_at = NOW() WHERE user_id = $1`, userID); err != nil {
		return "", nil, "", fmt.Errorf("password reset consume spend: %w", err)
	}
	u = &model.User{}
	if err := scanUser(tx.QueryRowContext(ctx,
		`UPDATE users SET password_hash = $2, session_version = session_version + 1,
		        email_verified_at = COALESCE(email_verified_at, NOW()), updated_at = NOW()
		  WHERE id = $1 RETURNING `+userColumns,
		userID, passwordHash,
	), u); err != nil {
		return "", nil, "", fmt.Errorf("password reset consume update user: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", nil, "", fmt.Errorf("password reset consume commit: %w", err)
	}
	return model.PasswordResetDone, u, issuedBy, nil
}
