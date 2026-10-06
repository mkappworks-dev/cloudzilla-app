package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// AvatarStore holds the avatar queries that span users and organizations.
type AvatarStore struct{ db *sql.DB }

func NewAvatarStore(db *sql.DB) *AvatarStore { return &AvatarStore{db: db} }

// WithOwnerLock runs fn while holding a transaction-scoped advisory lock for
// owner, such as "user:7". Object writes and deletes happen outside the
// database, so the row lock alone can't order two changes to one avatar.
func (s *AvatarStore) WithOwnerLock(ctx context.Context, owner string, fn func() error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("avatar lock begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('avatar'), hashtext($1))`, owner); err != nil {
		return fmt.Errorf("avatar lock: %w", err)
	}
	if err := fn(); err != nil {
		return err
	}
	return tx.Commit()
}

// KeyInUse reports whether the user or org with id currently points at key.
// kind is "user" or "org".
func (s *AvatarStore) KeyInUse(ctx context.Context, kind string, id int64, key string) (bool, error) {
	table := "users"
	if kind == "org" {
		table = "organizations"
	}
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM `+table+` WHERE id = $1 AND avatar_key = $2`, id, key).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("avatar key in use: %w", err)
	}
	return true, nil
}
