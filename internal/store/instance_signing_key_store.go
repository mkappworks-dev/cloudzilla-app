package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// InstanceSigningKeyStore provides database operations for the keys the server signs its own commits with.
type InstanceSigningKeyStore struct{ db *sql.DB }

func NewInstanceSigningKeyStore(db *sql.DB) *InstanceSigningKeyStore {
	return &InstanceSigningKeyStore{db: db}
}

const instanceKeyColumns = `id, public_key, fingerprint, sealed_private, created_at, retired_at`

func scanInstanceKey(row interface{ Scan(...any) error }) (*model.InstanceSigningKey, error) {
	var k model.InstanceSigningKey
	if err := row.Scan(&k.ID, &k.PublicKey, &k.Fingerprint, &k.SealedPrivate, &k.CreatedAt, &k.RetiredAt); err != nil {
		return nil, err
	}
	return &k, nil
}

// Active returns the key in use, or nil when none has been generated.
func (s *InstanceSigningKeyStore) Active(ctx context.Context) (*model.InstanceSigningKey, error) {
	k, err := scanInstanceKey(s.db.QueryRowContext(ctx,
		`SELECT `+instanceKeyColumns+` FROM instance_signing_keys WHERE retired_at IS NULL`))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("active instance signing key: %w", err)
	}
	return k, nil
}

// CreateIfNoActive stores key unless a key is already active; false means another caller got there first.
func (s *InstanceSigningKeyStore) CreateIfNoActive(ctx context.Context, key *model.InstanceSigningKey) (bool, error) {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO instance_signing_keys (public_key, fingerprint, sealed_private)
		 VALUES ($1, $2, $3) ON CONFLICT DO NOTHING RETURNING id, created_at`,
		key.PublicKey, key.Fingerprint, key.SealedPrivate,
	).Scan(&key.ID, &key.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("instance signing key create: %w", err)
	}
	return true, nil
}

// Rotate retires the active key, if any, and stores key as the new active one.
func (s *InstanceSigningKeyStore) Rotate(ctx context.Context, key *model.InstanceSigningKey) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Serialises rotations: a second one would otherwise retire nothing and then collide on the active index.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('instance_signing_keys'))`); err != nil {
		return fmt.Errorf("instance signing key lock: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE instance_signing_keys SET retired_at = NOW() WHERE retired_at IS NULL`); err != nil {
		return fmt.Errorf("instance signing key retire: %w", err)
	}
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO instance_signing_keys (public_key, fingerprint, sealed_private)
		 VALUES ($1, $2, $3) RETURNING id, created_at`,
		key.PublicKey, key.Fingerprint, key.SealedPrivate,
	).Scan(&key.ID, &key.CreatedAt); err != nil {
		return fmt.Errorf("instance signing key create: %w", err)
	}
	return tx.Commit()
}

// ListByFingerprints returns the active and retired keys with any of the SHA256 fingerprints, in one query.
func (s *InstanceSigningKeyStore) ListByFingerprints(ctx context.Context, fingerprints []string) ([]model.InstanceSigningKey, error) {
	if len(fingerprints) == 0 {
		return nil, nil
	}
	ph, args := textPlaceholders(1, fingerprints)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+instanceKeyColumns+` FROM instance_signing_keys WHERE fingerprint IN (`+ph+`) ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("instance signing keys by fingerprint: %w", err)
	}
	defer rows.Close()
	var keys []model.InstanceSigningKey
	for rows.Next() {
		k, err := scanInstanceKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, *k)
	}
	return keys, rows.Err()
}
