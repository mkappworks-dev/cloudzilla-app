package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
)

type PullLineCommentStore struct{ db *sql.DB }

func NewPullLineCommentStore(db *sql.DB) *PullLineCommentStore {
	return &PullLineCommentStore{db: db}
}

func (s *PullLineCommentStore) Create(ctx context.Context, c *model.PullLineComment) error {
	const q = `
INSERT INTO pull_line_comments (pull_id, repo_id, author_id, author_name, path, diff_side, line, body)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, created_at, updated_at`
	return s.db.QueryRowContext(ctx, q,
		c.PullID, c.RepoID, c.AuthorID, c.AuthorName, c.Path, c.DiffSide, c.Line, c.Body,
	).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
}

func (s *PullLineCommentStore) ListByPull(ctx context.Context, pullID int64) ([]model.PullLineComment, error) {
	const q = `
SELECT id, pull_id, repo_id, author_id, author_name, path, diff_side, line, body, created_at, updated_at
FROM pull_line_comments WHERE pull_id=$1 ORDER BY path, line, created_at`
	rows, err := s.db.QueryContext(ctx, q, pullID)
	if err != nil {
		return nil, fmt.Errorf("pull line comments list: %w", err)
	}
	defer rows.Close()
	var comments []model.PullLineComment
	for rows.Next() {
		var c model.PullLineComment
		if err := rows.Scan(
			&c.ID, &c.PullID, &c.RepoID, &c.AuthorID, &c.AuthorName,
			&c.Path, &c.DiffSide, &c.Line, &c.Body, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

func (s *PullLineCommentStore) GetByID(ctx context.Context, id int64) (*model.PullLineComment, error) {
	const q = `
SELECT id, pull_id, repo_id, author_id, author_name, path, diff_side, line, body, created_at, updated_at
FROM pull_line_comments WHERE id=$1`
	var c model.PullLineComment
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&c.ID, &c.PullID, &c.RepoID, &c.AuthorID, &c.AuthorName,
		&c.Path, &c.DiffSide, &c.Line, &c.Body, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("pull line comment get: %w", err)
	}
	return &c, nil
}

func (s *PullLineCommentStore) Delete(ctx context.Context, id, repoID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM pull_line_comments WHERE id=$1 AND repo_id=$2`,
		id, repoID,
	)
	return err
}
