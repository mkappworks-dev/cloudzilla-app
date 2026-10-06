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
// and session version, and returns that email. An empty tokenHash records an
// email that carried no link. A cooldown above zero marks an emailed issue: it
// is refused while the last email is newer than the cooldown, or while a link
// an admin issued is still usable, so the forgot form can't cancel that link.
func (s *PasswordResetStore) Issue(ctx context.Context, userID int64, tokenHash, issuedBy string, ttl, cooldown time.Duration) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("password reset issue begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var email string
	var sessionVersion int
	if err := tx.QueryRowContext(ctx,
		`SELECT email, session_version FROM users WHERE id = $1 FOR UPDATE`, userID,
	).Scan(&email, &sessionVersion); err != nil {
		return "", fmt.Errorf("password reset issue lock user: %w", err)
	}
	if cooldown > 0 {
		var blocked bool
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM password_reset_tokens t JOIN users u ON u.id = t.user_id
			                 WHERE t.user_id = $1
			                   AND (t.emailed_at > NOW() - make_interval(secs => $2)
			                        OR (t.issued_by <> 'email' AND t.expires_at > NOW() AND `+passwordResetUsable+`)))`,
			userID, cooldown.Seconds(),
		).Scan(&blocked); err != nil {
			return "", fmt.Errorf("password reset issue cooldown: %w", err)
		}
		if blocked {
			return "", ErrPasswordResetCooldown
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO password_reset_tokens (user_id, email, token_hash, session_version, issued_by, expires_at, emailed_at)
		 VALUES ($1, $2, NULLIF($3, ''), $4, $5, NOW() + make_interval(secs => $6), CASE WHEN $5 = 'email' THEN NOW() END)
		 ON CONFLICT (user_id) DO UPDATE
		   SET email = EXCLUDED.email, token_hash = EXCLUDED.token_hash, session_version = EXCLUDED.session_version,
		       issued_by = EXCLUDED.issued_by, created_at = NOW(), expires_at = EXCLUDED.expires_at, used_at = NULL,
		       emailed_at = COALESCE(EXCLUDED.emailed_at, password_reset_tokens.emailed_at)`,
		userID, email, tokenHash, sessionVersion, issuedBy, ttl.Seconds(),
	); err != nil {
		return "", fmt.Errorf("password reset issue upsert: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("password reset issue commit: %w", err)
	}
	return email, nil
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

// Consume spends tokenHash: it sets passwordHash and ends every session. An
// emailed link also verifies its address, since only the mailbox's owner could
// open it; one an admin handed over proves nothing about the address. The user and who issued the link
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
		        email_verified_at = CASE WHEN $3 = 'email' THEN COALESCE(email_verified_at, NOW()) ELSE email_verified_at END,
		        updated_at = NOW()
		  WHERE id = $1 RETURNING `+userColumns,
		userID, passwordHash, issuedBy,
	), u); err != nil {
		return "", nil, "", fmt.Errorf("password reset consume update user: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", nil, "", fmt.Errorf("password reset consume commit: %w", err)
	}
	return model.PasswordResetDone, u, issuedBy, nil
}
