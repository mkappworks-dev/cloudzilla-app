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

var (
	ErrReleaseAssetNameTaken = errors.New("release asset name already in use")
	ErrReleaseAssetLimit     = errors.New("release asset limit reached")
)

type ReleaseAssetStore struct{ db *sql.DB }

func NewReleaseAssetStore(db *sql.DB) *ReleaseAssetStore { return &ReleaseAssetStore{db: db} }

const releaseAssetColumns = `id, release_id, repo_id, name, size, sha256, storage_key, uploader_id, created_at`

// Create refuses past max assets. It locks the release row so two concurrent
// uploads can't both pass the count.
func (s *ReleaseAssetStore) Create(ctx context.Context, a *model.ReleaseAsset, max int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("release asset create begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var locked int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM releases WHERE id = $1 FOR UPDATE`, a.ReleaseID).Scan(&locked); err != nil {
		return fmt.Errorf("release asset lock release: %w", err)
	}
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM release_assets WHERE release_id = $1`, a.ReleaseID).Scan(&n); err != nil {
		return fmt.Errorf("release asset count: %w", err)
	}
	if n >= max {
		return ErrReleaseAssetLimit
	}
	err = tx.QueryRowContext(ctx,
		`INSERT INTO release_assets (release_id, repo_id, name, size, sha256, storage_key, uploader_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, created_at`,
		a.ReleaseID, a.RepoID, a.Name, a.Size, a.SHA256, a.StorageKey, a.UploaderID,
	).Scan(&a.ID, &a.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrReleaseAssetNameTaken
		}
		return fmt.Errorf("release asset create: %w", err)
	}
	return tx.Commit()
}

func (s *ReleaseAssetStore) Count(ctx context.Context, releaseID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM release_assets WHERE release_id = $1`, releaseID).Scan(&n)
	return n, err
}

func (s *ReleaseAssetStore) NameExists(ctx context.Context, releaseID int64, name string) (bool, error) {
	var ok bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM release_assets WHERE release_id = $1 AND name = $2)`, releaseID, name).Scan(&ok)
	return ok, err
}

func (s *ReleaseAssetStore) Get(ctx context.Context, releaseID, id int64) (*model.ReleaseAsset, error) {
	return scanReleaseAsset(s.db.QueryRowContext(ctx,
		`SELECT `+releaseAssetColumns+` FROM release_assets WHERE id = $1 AND release_id = $2`, id, releaseID))
}

func (s *ReleaseAssetStore) GetByName(ctx context.Context, releaseID int64, name string) (*model.ReleaseAsset, error) {
	return scanReleaseAsset(s.db.QueryRowContext(ctx,
		`SELECT `+releaseAssetColumns+` FROM release_assets WHERE release_id = $1 AND name = $2`, releaseID, name))
}

func (s *ReleaseAssetStore) ListByRelease(ctx context.Context, releaseID int64) ([]model.ReleaseAsset, error) {
	return s.list(ctx, `WHERE release_id = $1 ORDER BY created_at, id`, releaseID)
}

func (s *ReleaseAssetStore) ListByReleases(ctx context.Context, releaseIDs []int64) ([]model.ReleaseAsset, error) {
	if len(releaseIDs) == 0 {
		return nil, nil
	}
	return s.list(ctx, `WHERE release_id = ANY($1) ORDER BY created_at, id`, releaseIDs)
}

// ListByReposDeletedBefore finds the assets of repos a purge with the same
// cutoff is about to delete, which cascade-deletes their rows.
func (s *ReleaseAssetStore) ListByReposDeletedBefore(ctx context.Context, cutoff time.Time) ([]model.ReleaseAsset, error) {
	return s.list(ctx,
		`WHERE repo_id IN (SELECT id FROM repositories WHERE deleted_at IS NOT NULL AND deleted_at < $1)`, cutoff)
}

func (s *ReleaseAssetStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM release_assets WHERE id = $1`, id)
	return err
}

func (s *ReleaseAssetStore) list(ctx context.Context, where string, args ...any) ([]model.ReleaseAsset, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+releaseAssetColumns+` FROM release_assets `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("release asset list: %w", err)
	}
	defer rows.Close()
	var out []model.ReleaseAsset
	for rows.Next() {
		a, err := scanReleaseAsset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func scanReleaseAsset(row rowScanner) (*model.ReleaseAsset, error) {
	var a model.ReleaseAsset
	var uploader sql.NullInt64
	if err := row.Scan(&a.ID, &a.ReleaseID, &a.RepoID, &a.Name, &a.Size, &a.SHA256, &a.StorageKey, &uploader, &a.CreatedAt); err != nil {
		return nil, err
	}
	if uploader.Valid {
		a.UploaderID = &uploader.Int64
	}
	return &a, nil
}
