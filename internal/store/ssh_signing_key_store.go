package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// SSHSigningKeyStore provides database operations for SSH keys that sign commits.
type SSHSigningKeyStore struct{ db *sql.DB }

func NewSSHSigningKeyStore(db *sql.DB) *SSHSigningKeyStore { return &SSHSigningKeyStore{db: db} }

func (s *SSHSigningKeyStore) Create(ctx context.Context, key *model.SSHSigningKey) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO ssh_signing_keys (user_id, title, public_key, fingerprint)
		 VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
		key.UserID, key.Title, key.PublicKey, key.Fingerprint,
	).Scan(&key.ID, &key.CreatedAt)
	if err != nil {
		return signingKeyInsertError("ssh signing key create", err)
	}
	return nil
}

func (s *SSHSigningKeyStore) ListByUser(ctx context.Context, userID int64) ([]model.SSHSigningKey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, title, public_key, fingerprint, created_at
		 FROM ssh_signing_keys WHERE user_id = $1 ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("ssh signing keys list: %w", err)
	}
	defer rows.Close()
	var keys []model.SSHSigningKey
	for rows.Next() {
		var k model.SSHSigningKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Title, &k.PublicKey, &k.Fingerprint, &k.CreatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// Delete removes userID's key; false means no such key of theirs.
func (s *SSHSigningKeyStore) Delete(ctx context.Context, keyID, userID int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM ssh_signing_keys WHERE id = $1 AND user_id = $2`, keyID, userID)
	if err != nil {
		return false, fmt.Errorf("ssh signing key delete: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
