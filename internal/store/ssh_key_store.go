package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
)

type SSHKeyStore struct{ db *sql.DB }

func NewSSHKeyStore(db *sql.DB) *SSHKeyStore { return &SSHKeyStore{db: db} }

func (s *SSHKeyStore) Create(ctx context.Context, key *model.SSHKey) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO ssh_keys (user_id, title, public_key, fingerprint)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, created_at`,
		key.UserID, key.Title, key.PublicKey, key.Fingerprint,
	).Scan(&key.ID, &key.CreatedAt)
	if err != nil {
		return fmt.Errorf("ssh key create: %w", err)
	}
	return nil
}

func (s *SSHKeyStore) ListByUser(ctx context.Context, userID int64) ([]model.SSHKey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, title, public_key, fingerprint, created_at
		 FROM ssh_keys WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("ssh keys list by user: %w", err)
	}
	defer rows.Close()
	var keys []model.SSHKey
	for rows.Next() {
		var k model.SSHKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Title, &k.PublicKey, &k.Fingerprint, &k.CreatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (s *SSHKeyStore) GetByFingerprint(ctx context.Context, fingerprint string) (*model.SSHKey, error) {
	var k model.SSHKey
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, title, public_key, fingerprint, created_at
		 FROM ssh_keys WHERE fingerprint = $1`,
		fingerprint,
	).Scan(&k.ID, &k.UserID, &k.Title, &k.PublicKey, &k.Fingerprint, &k.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("ssh key get by fingerprint: %w", err)
	}
	return &k, nil
}

func (s *SSHKeyStore) Delete(ctx context.Context, keyID, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM ssh_keys WHERE id = $1 AND user_id = $2`,
		keyID, userID,
	)
	if err != nil {
		return fmt.Errorf("ssh key delete: %w", err)
	}
	return nil
}
