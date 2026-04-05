package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
)

// GistStore provides database operations for gists and their files.
type GistStore struct{ db *sql.DB }

// NewGistStore creates a GistStore backed by the given database.
func NewGistStore(db *sql.DB) *GistStore { return &GistStore{db: db} }

func (s *GistStore) Create(ctx context.Context, g *model.Gist, files []model.GistFile) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("gist create begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		`INSERT INTO gists (id, owner_id, owner_name, description, public) VALUES ($1, $2, $3, $4, $5)`,
		g.ID, g.OwnerID, g.OwnerName, g.Description, g.Public,
	)
	if err != nil {
		return fmt.Errorf("gist insert: %w", err)
	}

	for _, f := range files {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO gist_files (gist_id, filename, content) VALUES ($1, $2, $3)`,
			g.ID, f.Filename, f.Content,
		)
		if err != nil {
			return fmt.Errorf("gist file insert: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("gist create commit: %w", err)
	}
	return nil
}

func (s *GistStore) Get(ctx context.Context, id string) (*model.Gist, []model.GistFile, error) {
	g := &model.Gist{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, owner_id, owner_name, description, public, created_at, updated_at FROM gists WHERE id = $1`,
		id,
	).Scan(&g.ID, &g.OwnerID, &g.OwnerName, &g.Description, &g.Public, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		return nil, nil, fmt.Errorf("gist get: %w", err)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, gist_id, filename, content FROM gist_files WHERE gist_id = $1 ORDER BY id`,
		id,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("gist files get: %w", err)
	}
	defer rows.Close()
	var files []model.GistFile
	for rows.Next() {
		var f model.GistFile
		if err := rows.Scan(&f.ID, &f.GistID, &f.Filename, &f.Content); err != nil {
			return nil, nil, err
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return g, files, nil
}

func (s *GistStore) ListByOwner(ctx context.Context, ownerID int64, page, pageSize int) ([]model.Gist, error) {
	offset := (page - 1) * pageSize
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_id, owner_name, description, public, created_at, updated_at
         FROM gists WHERE owner_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		ownerID, pageSize, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("gist list by owner: %w", err)
	}
	defer rows.Close()
	return scanGists(rows)
}

func (s *GistStore) ListPublicByOwner(ctx context.Context, ownerID int64, page, pageSize int) ([]model.Gist, error) {
	offset := (page - 1) * pageSize
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_id, owner_name, description, public, created_at, updated_at
         FROM gists WHERE owner_id = $1 AND public = true ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		ownerID, pageSize, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("gist list public by owner: %w", err)
	}
	defer rows.Close()
	return scanGists(rows)
}

func (s *GistStore) ListPublic(ctx context.Context, page, pageSize int) ([]model.Gist, error) {
	offset := (page - 1) * pageSize
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_id, owner_name, description, public, created_at, updated_at
         FROM gists WHERE public = true ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
		pageSize, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("gist list public: %w", err)
	}
	defer rows.Close()
	return scanGists(rows)
}

func (s *GistStore) Update(ctx context.Context, g *model.Gist, files []model.GistFile) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("gist update begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		`UPDATE gists SET description=$1, public=$2, updated_at=NOW() WHERE id=$3`,
		g.Description, g.Public, g.ID,
	)
	if err != nil {
		return fmt.Errorf("gist update: %w", err)
	}

	_, err = tx.ExecContext(ctx, `DELETE FROM gist_files WHERE gist_id = $1`, g.ID)
	if err != nil {
		return fmt.Errorf("gist files delete: %w", err)
	}

	for _, f := range files {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO gist_files (gist_id, filename, content) VALUES ($1, $2, $3)`,
			g.ID, f.Filename, f.Content,
		)
		if err != nil {
			return fmt.Errorf("gist file insert: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("gist update commit: %w", err)
	}
	return nil
}

func (s *GistStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM gists WHERE id = $1`, id)
	return err
}

func scanGists(rows *sql.Rows) ([]model.Gist, error) {
	var gists []model.Gist
	for rows.Next() {
		var g model.Gist
		if err := rows.Scan(&g.ID, &g.OwnerID, &g.OwnerName, &g.Description, &g.Public, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, err
		}
		gists = append(gists, g)
	}
	return gists, rows.Err()
}
