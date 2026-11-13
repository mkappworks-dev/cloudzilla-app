package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// CISecretStore persists sealed repo secrets.
type CISecretStore struct{ db *sql.DB }

func NewCISecretStore(db *sql.DB) *CISecretStore { return &CISecretStore{db: db} }

// SecretRow is a secret as stored: the value is still sealed.
type SecretRow struct {
	ID            int64
	Name          string
	ValueEnc      []byte
	CreatedBy     *int64
	CreatedByName string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Upsert stores a new secret or replaces the value of the one with this name, keeping its creator.
func (s *CISecretStore) Upsert(ctx context.Context, repoID int64, name string, sealed []byte, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO ci_secrets (repo_id, name, value_enc, created_by) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (repo_id, name) DO UPDATE SET value_enc = EXCLUDED.value_enc, updated_at = NOW()`,
		repoID, name, sealed, userID)
	if err != nil {
		return fmt.Errorf("upsert ci secret: %w", err)
	}
	return nil
}

func (s *CISecretStore) Delete(ctx context.Context, repoID int64, name string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM ci_secrets WHERE repo_id = $1 AND name = $2`, repoID, name)
	if err != nil {
		return false, fmt.Errorf("delete ci secret: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *CISecretStore) Count(ctx context.Context, repoID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ci_secrets WHERE repo_id = $1`, repoID).Scan(&n)
	return n, err
}

func (s *CISecretStore) Exists(ctx context.Context, repoID int64, name string) (bool, error) {
	var ok bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM ci_secrets WHERE repo_id = $1 AND name = $2)`, repoID, name).Scan(&ok)
	return ok, err
}

// List returns the repo's secrets by name. withValues also loads the sealed values.
func (s *CISecretStore) List(ctx context.Context, repoID int64, withValues bool) ([]SecretRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT s.id, s.name, CASE WHEN $2 THEN s.value_enc ELSE ''::bytea END, s.created_by, COALESCE(u.username, ''), s.created_at, s.updated_at
		   FROM ci_secrets s LEFT JOIN users u ON u.id = s.created_by
		  WHERE s.repo_id = $1 ORDER BY s.name`, repoID, withValues)
	if err != nil {
		return nil, fmt.Errorf("list ci secrets: %w", err)
	}
	defer rows.Close()
	var out []SecretRow
	for rows.Next() {
		var r SecretRow
		if err := rows.Scan(&r.ID, &r.Name, &r.ValueEnc, &r.CreatedBy, &r.CreatedByName, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
