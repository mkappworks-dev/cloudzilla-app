package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

var ErrTooManyEmails = errors.New("too many additional email addresses")

const userEmailsEmailLowerKey = "user_emails_email_lower_key"

// UserEmailStore keeps the addresses beside users.email and their verification link tokens by SHA-256.
type UserEmailStore struct {
	db *sql.DB
}

func NewUserEmailStore(database *sql.DB) *UserEmailStore {
	return &UserEmailStore{db: database}
}

// Add claims email for userID and stores tokenHash as its only link. The users row lock serializes one user's
// adds, so the cap and the cooldown cannot be raced past; a taken address (primary or additional, any case)
// is ErrEmailTaken.
func (s *UserEmailStore) Add(ctx context.Context, userID int64, email, tokenHash string, ttl, cooldown time.Duration, max int) (*model.UserEmail, string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, "", fmt.Errorf("user email add begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var username string
	if err := tx.QueryRowContext(ctx, `SELECT username FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&username); err != nil {
		return nil, "", fmt.Errorf("user email add lock user: %w", err)
	}
	var count int
	var recent bool
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(bool_or(created_at > NOW() - make_interval(secs => $2)), false)
		   FROM user_emails WHERE user_id = $1`,
		userID, cooldown.Seconds(),
	).Scan(&count, &recent); err != nil {
		return nil, "", fmt.Errorf("user email add count: %w", err)
	}
	if count >= max {
		return nil, "", ErrTooManyEmails
	}
	if recent {
		return nil, "", ErrVerificationCooldown
	}
	e := &model.UserEmail{}
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO user_emails (user_id, email) VALUES ($1, $2) RETURNING id, user_id, email, verified_at, created_at`,
		userID, email,
	).Scan(&e.ID, &e.UserID, &e.Email, &e.VerifiedAt, &e.CreatedAt); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
			(pgErr.ConstraintName == userEmailsEmailLowerKey || pgErr.ConstraintName == usersEmailLowerKey) {
			return nil, "", ErrEmailTaken
		}
		return nil, "", fmt.Errorf("user email add insert: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO user_email_tokens (user_email_id, token_hash, expires_at) VALUES ($1, $2, NOW() + make_interval(secs => $3))`,
		e.ID, tokenHash, ttl.Seconds(),
	); err != nil {
		return nil, "", fmt.Errorf("user email add token: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, "", fmt.Errorf("user email add commit: %w", err)
	}
	return e, username, nil
}

func (s *UserEmailStore) List(ctx context.Context, userID int64) ([]model.UserEmail, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, email, verified_at, created_at FROM user_emails WHERE user_id = $1 ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, fmt.Errorf("user email list: %w", err)
	}
	defer rows.Close()
	var out []model.UserEmail
	for rows.Next() {
		var e model.UserEmail
		if err := rows.Scan(&e.ID, &e.UserID, &e.Email, &e.VerifiedAt, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("user email list scan: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Remove reports whether userID owned the row; its tokens go with it.
func (s *UserEmailStore) Remove(ctx context.Context, userID, id int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM user_emails WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return false, fmt.Errorf("user email remove: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// Lookup reports what consuming tokenHash now would do, without consuming it.
func (s *UserEmailStore) Lookup(ctx context.Context, tokenHash string) (model.EmailVerificationLink, error) {
	var expired, used bool
	var link model.EmailVerificationLink
	err := s.db.QueryRowContext(ctx,
		`SELECT t.expires_at <= NOW(), t.used_at IS NOT NULL, u.username, e.email
		   FROM user_email_tokens t JOIN user_emails e ON e.id = t.user_email_id JOIN users u ON u.id = e.user_id
		  WHERE t.token_hash = $1`,
		tokenHash,
	).Scan(&expired, &used, &link.Username, &link.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return model.EmailVerificationLink{State: model.EmailVerificationInvalid}, nil
	}
	if err != nil {
		return model.EmailVerificationLink{}, fmt.Errorf("user email lookup: %w", err)
	}
	link.State = tokenState(expired, used, true)
	if link.State != model.EmailVerificationPending {
		return model.EmailVerificationLink{State: link.State}, nil
	}
	return link, nil
}

// Consume verifies the address behind tokenHash. The row is returned only when the result is EmailVerificationVerified.
func (s *UserEmailStore) Consume(ctx context.Context, tokenHash string) (model.EmailVerificationState, *model.UserEmail, string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", nil, "", fmt.Errorf("user email consume begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var tokenID, emailID int64
	var expired, used bool
	// FOR UPDATE: a concurrent consume of the same link waits, then sees used_at.
	err = tx.QueryRowContext(ctx,
		`SELECT id, user_email_id, expires_at <= NOW(), used_at IS NOT NULL
		   FROM user_email_tokens WHERE token_hash = $1 FOR UPDATE`, tokenHash,
	).Scan(&tokenID, &emailID, &expired, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return model.EmailVerificationInvalid, nil, "", nil
	}
	if err != nil {
		return "", nil, "", fmt.Errorf("user email consume find: %w", err)
	}
	if state := tokenState(expired, used, true); state != model.EmailVerificationPending {
		return state, nil, "", nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE user_email_tokens SET used_at = NOW() WHERE user_email_id = $1 AND used_at IS NULL`, emailID); err != nil {
		return "", nil, "", fmt.Errorf("user email consume spend: %w", err)
	}
	e := &model.UserEmail{}
	var username string
	if err := tx.QueryRowContext(ctx,
		`WITH v AS (UPDATE user_emails SET verified_at = COALESCE(verified_at, NOW()) WHERE id = $1
		            RETURNING id, user_id, email, verified_at, created_at)
		 SELECT v.id, v.user_id, v.email, v.verified_at, v.created_at, u.username FROM v JOIN users u ON u.id = v.user_id`, emailID,
	).Scan(&e.ID, &e.UserID, &e.Email, &e.VerifiedAt, &e.CreatedAt, &username); err != nil {
		return "", nil, "", fmt.Errorf("user email consume verify: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", nil, "", fmt.Errorf("user email consume commit: %w", err)
	}
	return model.EmailVerificationVerified, e, username, nil
}
