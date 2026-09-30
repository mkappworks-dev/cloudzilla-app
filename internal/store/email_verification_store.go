package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

var (
	ErrEmailAlreadyVerified = errors.New("email is already verified")
	ErrVerificationCooldown = errors.New("a verification email was sent too recently")
)

// EmailVerificationStore keeps email verification tokens by their SHA-256.
// Methods that touch both tables lock the users row first, so they cannot
// deadlock with UserStore.UpdateProfile.
type EmailVerificationStore struct {
	db *sql.DB
}

func NewEmailVerificationStore(database *sql.DB) *EmailVerificationStore {
	return &EmailVerificationStore{db: database}
}

// Issue makes tokenHash the user's only live token, bound to their current
// email, and returns that email and the username. The cooldown counts every
// link to the address as well as the user's own, so recreating the account or
// moving the address to another one doesn't reset it.
func (s *EmailVerificationStore) Issue(ctx context.Context, userID int64, tokenHash string, ttl, cooldown time.Duration) (email, username string, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", fmt.Errorf("email verification issue begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var verified bool
	if err := tx.QueryRowContext(ctx,
		`SELECT email, username, email_verified_at IS NOT NULL FROM users WHERE id = $1 FOR UPDATE`, userID,
	).Scan(&email, &username, &verified); err != nil {
		return "", "", fmt.Errorf("email verification issue lock user: %w", err)
	}
	if verified {
		return "", "", ErrEmailAlreadyVerified
	}
	// The user row lock doesn't cover another account holding a case variant of the address.
	if _, err := tx.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(hashtext('email_verification'), hashtext(lower($1)))`, email,
	); err != nil {
		return "", "", fmt.Errorf("email verification issue lock address: %w", err)
	}
	var recent bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM email_verification_tokens
		                 WHERE (user_id = $1 OR lower(email) = lower($2))
		                   AND created_at > NOW() - make_interval(secs => $3))`,
		userID, email, cooldown.Seconds(),
	).Scan(&recent); err != nil {
		return "", "", fmt.Errorf("email verification issue cooldown: %w", err)
	}
	if recent {
		return "", "", ErrVerificationCooldown
	}
	// Every remaining row of the user's is older than the cooldown, so none is still needed.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM email_verification_tokens
		  WHERE user_id = $1 OR (user_id IS NULL AND created_at <= NOW() - make_interval(secs => $2))`,
		userID, cooldown.Seconds(),
	); err != nil {
		return "", "", fmt.Errorf("email verification issue revoke: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO email_verification_tokens (user_id, email, token_hash, expires_at)
		 VALUES ($1, $2, $3, NOW() + make_interval(secs => $4))`,
		userID, email, tokenHash, ttl.Seconds(),
	); err != nil {
		return "", "", fmt.Errorf("email verification issue insert: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", "", fmt.Errorf("email verification issue commit: %w", err)
	}
	return email, username, nil
}

// Lookup reports what consuming tokenHash now would do, without consuming it.
func (s *EmailVerificationStore) Lookup(ctx context.Context, tokenHash string) (model.EmailVerificationLink, error) {
	var expired, used, emailMatches bool
	var link model.EmailVerificationLink
	err := s.db.QueryRowContext(ctx,
		`SELECT t.expires_at <= NOW(), t.used_at IS NOT NULL, t.email = u.email, u.username, t.email
		   FROM email_verification_tokens t JOIN users u ON u.id = t.user_id
		  WHERE t.token_hash = $1`,
		tokenHash,
	).Scan(&expired, &used, &emailMatches, &link.Username, &link.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return model.EmailVerificationLink{State: model.EmailVerificationInvalid}, nil
	}
	if err != nil {
		return model.EmailVerificationLink{}, fmt.Errorf("email verification lookup: %w", err)
	}
	link.State = tokenState(expired, used, emailMatches)
	if link.State != model.EmailVerificationPending {
		return model.EmailVerificationLink{State: link.State}, nil
	}
	return link, nil
}

// LinkPending reports whether userID holds a live link for their current address.
func (s *EmailVerificationStore) LinkPending(ctx context.Context, userID int64) (bool, error) {
	var pending bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM email_verification_tokens t JOIN users u ON u.id = t.user_id
		                 WHERE t.user_id = $1 AND t.used_at IS NULL AND t.expires_at > NOW() AND t.email = u.email)`,
		userID,
	).Scan(&pending)
	if err != nil {
		return false, fmt.Errorf("email verification link pending: %w", err)
	}
	return pending, nil
}

func tokenState(expired, used, emailMatches bool) model.EmailVerificationState {
	switch {
	case used || !emailMatches:
		return model.EmailVerificationInvalid
	case expired:
		return model.EmailVerificationExpired
	}
	return model.EmailVerificationPending
}

// Consume verifies the token's user and spends every live token they hold.
// The user is returned only when the result is EmailVerificationVerified.
func (s *EmailVerificationStore) Consume(ctx context.Context, tokenHash string) (model.EmailVerificationState, *model.User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", nil, fmt.Errorf("email verification consume begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var userID int64
	err = tx.QueryRowContext(ctx,
		`SELECT user_id FROM email_verification_tokens WHERE token_hash = $1 AND user_id IS NOT NULL`, tokenHash,
	).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return model.EmailVerificationInvalid, nil, nil
	}
	if err != nil {
		return "", nil, fmt.Errorf("email verification consume find: %w", err)
	}
	var userEmail string
	if err := tx.QueryRowContext(ctx, `SELECT email FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&userEmail); err != nil {
		return "", nil, fmt.Errorf("email verification consume lock user: %w", err)
	}
	var expired, used bool
	var tokenEmail string
	// Re-read under lock: a concurrent consume, issue or email change may have
	// spent or removed the token since the first read.
	err = tx.QueryRowContext(ctx,
		`SELECT email, expires_at <= NOW(), used_at IS NOT NULL
		   FROM email_verification_tokens WHERE token_hash = $1 AND user_id = $2 FOR UPDATE`,
		tokenHash, userID,
	).Scan(&tokenEmail, &expired, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return model.EmailVerificationInvalid, nil, nil
	}
	if err != nil {
		return "", nil, fmt.Errorf("email verification consume lock token: %w", err)
	}
	if state := tokenState(expired, used, tokenEmail == userEmail); state != model.EmailVerificationPending {
		return state, nil, nil
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE email_verification_tokens SET used_at = NOW() WHERE user_id = $1 AND used_at IS NULL`, userID,
	); err != nil {
		return "", nil, fmt.Errorf("email verification consume spend: %w", err)
	}
	u := &model.User{}
	if err := scanUser(tx.QueryRowContext(ctx,
		`UPDATE users SET email_verified_at = COALESCE(email_verified_at, NOW()), updated_at = NOW()
		  WHERE id = $1 RETURNING `+userColumns,
		userID,
	), u); err != nil {
		return "", nil, fmt.Errorf("email verification consume verify: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", nil, fmt.Errorf("email verification consume commit: %w", err)
	}
	return model.EmailVerificationVerified, u, nil
}
