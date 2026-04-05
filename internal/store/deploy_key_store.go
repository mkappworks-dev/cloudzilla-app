package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
)

// DeployKeyStore provides database operations for repository deploy keys.
type DeployKeyStore struct{ db *sql.DB }

// NewDeployKeyStore creates a DeployKeyStore backed by the given database.
func NewDeployKeyStore(db *sql.DB) *DeployKeyStore { return &DeployKeyStore{db: db} }

func (s *DeployKeyStore) Create(ctx context.Context, k *model.DeployKey) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO deploy_keys (repo_id, title, fingerprint, public_key, read_only)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		k.RepoID, k.Title, k.Fingerprint, k.PublicKey, k.ReadOnly,
	).Scan(&k.ID, &k.CreatedAt)
	if err != nil {
		return fmt.Errorf("deploy key create: %w", err)
	}
	return nil
}

func (s *DeployKeyStore) ListByRepo(ctx context.Context, repoID int64) ([]model.DeployKey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, repo_id, title, fingerprint, public_key, read_only, last_used_at, created_at
		 FROM deploy_keys WHERE repo_id = $1 ORDER BY created_at DESC`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("deploy key list by repo: %w", err)
	}
	defer rows.Close()
	var keys []model.DeployKey
	for rows.Next() {
		var k model.DeployKey
		if err := rows.Scan(&k.ID, &k.RepoID, &k.Title, &k.Fingerprint, &k.PublicKey,
			&k.ReadOnly, &k.LastUsedAt, &k.CreatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (s *DeployKeyStore) GetByFingerprint(ctx context.Context, fingerprint string) (*model.DeployKey, error) {
	k := &model.DeployKey{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, repo_id, title, fingerprint, public_key, read_only, last_used_at, created_at
		 FROM deploy_keys WHERE fingerprint = $1`,
		fingerprint,
	).Scan(&k.ID, &k.RepoID, &k.Title, &k.Fingerprint, &k.PublicKey,
		&k.ReadOnly, &k.LastUsedAt, &k.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("deploy key get by fingerprint: %w", err)
	}
	return k, nil
}

func (s *DeployKeyStore) UpdateLastUsed(ctx context.Context, keyID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE deploy_keys SET last_used_at = NOW() WHERE id = $1`,
		keyID,
	)
	return err
}

func (s *DeployKeyStore) Delete(ctx context.Context, keyID, repoID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM deploy_keys WHERE id = $1 AND repo_id = $2`,
		keyID, repoID,
	)
	return err
}
