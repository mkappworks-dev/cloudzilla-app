package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// ReleaseStore provides database operations for repository releases.
type ReleaseStore struct{ db *sql.DB }

// NewReleaseStore creates a ReleaseStore backed by the given database.
func NewReleaseStore(db *sql.DB) *ReleaseStore { return &ReleaseStore{db: db} }

func (s *ReleaseStore) Create(ctx context.Context, r *model.Release) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO releases (repo_id, tag_name, name, body, is_prerelease, is_draft, author_id, published_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, created_at, updated_at`,
		r.RepoID, r.TagName, r.Name, r.Body, r.IsPrerelease, r.IsDraft, r.AuthorID, r.PublishedAt,
	).Scan(&r.ID, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return fmt.Errorf("release create: %w", err)
	}
	return nil
}

func (s *ReleaseStore) ListByRepo(ctx context.Context, repoID int64) ([]model.Release, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, repo_id, tag_name, name, body, is_prerelease, is_draft, author_id, created_at, updated_at, published_at
		 FROM releases WHERE repo_id = $1 ORDER BY created_at DESC`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("release list: %w", err)
	}
	defer rows.Close()
	return scanReleases(rows)
}

// CountPublished returns the number of non-draft releases in a repo.
func (s *ReleaseStore) CountPublished(ctx context.Context, repoID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM releases WHERE repo_id = $1 AND is_draft = FALSE`,
		repoID,
	).Scan(&n)
	return n, err
}

func (s *ReleaseStore) GetByTag(ctx context.Context, repoID int64, tagName string) (*model.Release, error) {
	r := &model.Release{}
	var publishedAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, repo_id, tag_name, name, body, is_prerelease, is_draft, author_id, created_at, updated_at, published_at
		 FROM releases WHERE repo_id = $1 AND tag_name = $2`,
		repoID, tagName,
	).Scan(&r.ID, &r.RepoID, &r.TagName, &r.Name, &r.Body, &r.IsPrerelease, &r.IsDraft, &r.AuthorID, &r.CreatedAt, &r.UpdatedAt, &publishedAt)
	if err != nil {
		return nil, fmt.Errorf("release get by tag: %w", err)
	}
	if publishedAt.Valid {
		r.PublishedAt = &publishedAt.Time
	}
	return r, nil
}

func (s *ReleaseStore) GetByID(ctx context.Context, id, repoID int64) (*model.Release, error) {
	r := &model.Release{}
	var publishedAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, repo_id, tag_name, name, body, is_prerelease, is_draft, author_id, created_at, updated_at, published_at
		 FROM releases WHERE id = $1 AND repo_id = $2`,
		id, repoID,
	).Scan(&r.ID, &r.RepoID, &r.TagName, &r.Name, &r.Body, &r.IsPrerelease, &r.IsDraft, &r.AuthorID, &r.CreatedAt, &r.UpdatedAt, &publishedAt)
	if err != nil {
		return nil, fmt.Errorf("release get by id: %w", err)
	}
	if publishedAt.Valid {
		r.PublishedAt = &publishedAt.Time
	}
	return r, nil
}

func (s *ReleaseStore) GetLatest(ctx context.Context, repoID int64) (*model.Release, error) {
	r := &model.Release{}
	var publishedAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, repo_id, tag_name, name, body, is_prerelease, is_draft, author_id, created_at, updated_at, published_at
		 FROM releases WHERE repo_id = $1 AND is_draft = false
		 ORDER BY created_at DESC LIMIT 1`,
		repoID,
	).Scan(&r.ID, &r.RepoID, &r.TagName, &r.Name, &r.Body, &r.IsPrerelease, &r.IsDraft, &r.AuthorID, &r.CreatedAt, &r.UpdatedAt, &publishedAt)
	if err != nil {
		return nil, fmt.Errorf("release get latest: %w", err)
	}
	if publishedAt.Valid {
		r.PublishedAt = &publishedAt.Time
	}
	return r, nil
}

func (s *ReleaseStore) Update(ctx context.Context, r *model.Release) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE releases SET tag_name=$1, name=$2, body=$3, is_prerelease=$4, is_draft=$5, published_at=$6, updated_at=NOW()
		 WHERE id=$7 AND repo_id=$8`,
		r.TagName, r.Name, r.Body, r.IsPrerelease, r.IsDraft, r.PublishedAt, r.ID, r.RepoID,
	)
	if err != nil {
		return fmt.Errorf("release update: %w", err)
	}
	return nil
}

func (s *ReleaseStore) Delete(ctx context.Context, id, repoID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM releases WHERE id=$1 AND repo_id=$2`, id, repoID)
	if err != nil {
		return fmt.Errorf("release delete: %w", err)
	}
	return nil
}

func scanReleases(rows *sql.Rows) ([]model.Release, error) {
	var releases []model.Release
	for rows.Next() {
		var r model.Release
		var publishedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.RepoID, &r.TagName, &r.Name, &r.Body, &r.IsPrerelease, &r.IsDraft, &r.AuthorID, &r.CreatedAt, &r.UpdatedAt, &publishedAt); err != nil {
			return nil, err
		}
		if publishedAt.Valid {
			r.PublishedAt = &publishedAt.Time
		}
		releases = append(releases, r)
	}
	return releases, rows.Err()
}
