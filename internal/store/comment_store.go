package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// CommentStore provides database operations for issue and PR comments.
type CommentStore struct{ db *sql.DB }

// NewCommentStore creates a CommentStore backed by the given database.
func NewCommentStore(db *sql.DB) *CommentStore { return &CommentStore{db: db} }

func (s *CommentStore) Create(ctx context.Context, c *model.Comment) error {
	var issueID, pullID sql.NullInt64
	if c.IssueID != nil && *c.IssueID != 0 {
		issueID = sql.NullInt64{Int64: *c.IssueID, Valid: true}
	}
	if c.PullID != nil && *c.PullID != 0 {
		pullID = sql.NullInt64{Int64: *c.PullID, Valid: true}
	}

	err := s.db.QueryRowContext(ctx,
		`INSERT INTO comments (repo_id, issue_id, pull_id, author_id, author_name, body)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, repo_id, issue_id, pull_id, author_id, author_name, body, created_at, updated_at`,
		c.RepoID, issueID, pullID, c.AuthorID, c.AuthorName, c.Body,
	).Scan(&c.ID, &c.RepoID, &issueID, &pullID, &c.AuthorID, &c.AuthorName, &c.Body, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return fmt.Errorf("comment create: %w", err)
	}
	if issueID.Valid {
		c.IssueID = &issueID.Int64
	}
	if pullID.Valid {
		c.PullID = &pullID.Int64
	}
	return nil
}

func (s *CommentStore) GetByID(ctx context.Context, id int64) (*model.Comment, error) {
	var c model.Comment
	var issueID, pullID sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT c.id, c.repo_id, c.issue_id, c.pull_id, c.author_id, u.username AS author_name, c.body, c.created_at, c.updated_at
		 FROM comments c
		 JOIN users u ON c.author_id = u.id
		 WHERE c.id = $1`,
		id,
	).Scan(&c.ID, &c.RepoID, &issueID, &pullID, &c.AuthorID, &c.AuthorName, &c.Body, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("comment get by id: %w", err)
	}
	if issueID.Valid {
		c.IssueID = &issueID.Int64
	}
	if pullID.Valid {
		c.PullID = &pullID.Int64
	}
	return &c, nil
}

func (s *CommentStore) Update(ctx context.Context, id int64, body string) (*model.Comment, error) {
	var c model.Comment
	var issueID, pullID sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`UPDATE comments SET body = $2, updated_at = NOW() WHERE id = $1
		 RETURNING id, repo_id, issue_id, pull_id, author_id, author_name, body, created_at, updated_at`,
		id, body,
	).Scan(&c.ID, &c.RepoID, &issueID, &pullID, &c.AuthorID, &c.AuthorName, &c.Body, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("comment update: %w", err)
	}
	if issueID.Valid {
		c.IssueID = &issueID.Int64
	}
	if pullID.Valid {
		c.PullID = &pullID.Int64
	}
	return &c, nil
}

func (s *CommentStore) ListByIssue(ctx context.Context, issueID int64) ([]model.Comment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.repo_id, c.issue_id, c.pull_id, c.author_id, u.username AS author_name, c.body, c.created_at, c.updated_at
		 FROM comments c
		 JOIN users u ON c.author_id = u.id
		 WHERE c.issue_id = $1
		 ORDER BY c.created_at ASC`,
		issueID,
	)
	if err != nil {
		return nil, fmt.Errorf("comment list by issue: %w", err)
	}
	defer rows.Close()
	return scanCommentRows(rows)
}

func (s *CommentStore) ListByPull(ctx context.Context, pullID int64) ([]model.Comment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.repo_id, c.issue_id, c.pull_id, c.author_id, u.username AS author_name, c.body, c.created_at, c.updated_at
		 FROM comments c
		 JOIN users u ON c.author_id = u.id
		 WHERE c.pull_id = $1
		 ORDER BY c.created_at ASC`,
		pullID,
	)
	if err != nil {
		return nil, fmt.Errorf("comment list by pull: %w", err)
	}
	defer rows.Close()
	return scanCommentRows(rows)
}

func (s *CommentStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM comments WHERE id = $1`, id)
	return err
}

func scanCommentRows(rows *sql.Rows) ([]model.Comment, error) {
	var comments []model.Comment
	for rows.Next() {
		var c model.Comment
		var issueID, pullID sql.NullInt64
		if err := rows.Scan(&c.ID, &c.RepoID, &issueID, &pullID, &c.AuthorID, &c.AuthorName, &c.Body, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		if issueID.Valid {
			c.IssueID = &issueID.Int64
		}
		if pullID.Valid {
			c.PullID = &pullID.Int64
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}
