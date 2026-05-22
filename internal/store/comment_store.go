package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

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

// PRs with no comments are absent from the returned map.
func (s *CommentStore) CountByPullIDs(ctx context.Context, pullIDs []int64) (map[int64]int, error) {
	if len(pullIDs) == 0 {
		return map[int64]int{}, nil
	}
	placeholders := make([]string, len(pullIDs))
	args := make([]any, len(pullIDs))
	for i, id := range pullIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	query := fmt.Sprintf(
		`SELECT pull_id, COUNT(*) FROM comments
		 WHERE pull_id IN (%s) GROUP BY pull_id`,
		strings.Join(placeholders, ","),
	)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("comment count by pull ids: %w", err)
	}
	defer rows.Close()
	result := make(map[int64]int)
	for rows.Next() {
		var pullID int64
		var count int
		if err := rows.Scan(&pullID, &count); err != nil {
			return nil, err
		}
		result[pullID] = count
	}
	return result, rows.Err()
}

// Issues with no comments are absent from the returned map.
func (s *CommentStore) CountByIssueIDs(ctx context.Context, issueIDs []int64) (map[int64]int, error) {
	if len(issueIDs) == 0 {
		return map[int64]int{}, nil
	}
	placeholders := make([]string, len(issueIDs))
	args := make([]any, len(issueIDs))
	for i, id := range issueIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	query := fmt.Sprintf(
		`SELECT issue_id, COUNT(*) FROM comments
		 WHERE issue_id IN (%s) GROUP BY issue_id`,
		strings.Join(placeholders, ","),
	)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("comment count by issue ids: %w", err)
	}
	defer rows.Close()
	result := make(map[int64]int)
	for rows.Next() {
		var issueID int64
		var count int
		if err := rows.Scan(&issueID, &count); err != nil {
			return nil, err
		}
		result[issueID] = count
	}
	return result, rows.Err()
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
