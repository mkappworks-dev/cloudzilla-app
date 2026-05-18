package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// PullReviewStore provides database operations for pull request reviews.
type PullReviewStore struct{ db *sql.DB }

// NewPullReviewStore creates a PullReviewStore backed by the given database.
func NewPullReviewStore(db *sql.DB) *PullReviewStore { return &PullReviewStore{db: db} }

func (s *PullReviewStore) Upsert(ctx context.Context, r *model.PullReview) error {
	const q = `
INSERT INTO pull_reviews (pull_id, repo_id, author_id, author_name, state, body, submitted_at)
VALUES ($1, $2, $3, $4, $5, $6, NOW())
ON CONFLICT (pull_id, author_id) DO UPDATE
  SET state=$5, body=$6, submitted_at=NOW(), updated_at=NOW()
RETURNING id, submitted_at, created_at, updated_at`
	return s.db.QueryRowContext(ctx, q,
		r.PullID, r.RepoID, r.AuthorID, r.AuthorName, r.State, r.Body,
	).Scan(&r.ID, &r.SubmittedAt, &r.CreatedAt, &r.UpdatedAt)
}

func (s *PullReviewStore) ListByPull(ctx context.Context, pullID int64) ([]model.PullReview, error) {
	const q = `
SELECT id, pull_id, repo_id, author_id, author_name, state, body, submitted_at, created_at, updated_at
FROM pull_reviews WHERE pull_id=$1 ORDER BY created_at ASC`
	rows, err := s.db.QueryContext(ctx, q, pullID)
	if err != nil {
		return nil, fmt.Errorf("pull reviews list: %w", err)
	}
	defer rows.Close()
	var reviews []model.PullReview
	for rows.Next() {
		var r model.PullReview
		var submittedAt sql.NullTime
		if err := rows.Scan(
			&r.ID, &r.PullID, &r.RepoID, &r.AuthorID, &r.AuthorName,
			&r.State, &r.Body, &submittedAt, &r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if submittedAt.Valid {
			t := submittedAt.Time
			r.SubmittedAt = &t
		}
		reviews = append(reviews, r)
	}
	return reviews, rows.Err()
}

func (s *PullReviewStore) ListByPullIDs(ctx context.Context, pullIDs []int64) (map[int64][]model.PullReview, error) {
	if len(pullIDs) == 0 {
		return map[int64][]model.PullReview{}, nil
	}
	placeholders := make([]string, len(pullIDs))
	args := make([]any, len(pullIDs))
	for i, id := range pullIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	q := fmt.Sprintf(
		`SELECT id, pull_id, repo_id, author_id, author_name, state, body, submitted_at, created_at, updated_at
		 FROM pull_reviews WHERE pull_id IN (%s) ORDER BY created_at ASC`,
		strings.Join(placeholders, ","),
	)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("pull reviews list by ids: %w", err)
	}
	defer rows.Close()
	result := make(map[int64][]model.PullReview)
	for rows.Next() {
		var r model.PullReview
		var submittedAt sql.NullTime
		if err := rows.Scan(
			&r.ID, &r.PullID, &r.RepoID, &r.AuthorID, &r.AuthorName,
			&r.State, &r.Body, &submittedAt, &r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if submittedAt.Valid {
			t := submittedAt.Time
			r.SubmittedAt = &t
		}
		result[r.PullID] = append(result[r.PullID], r)
	}
	return result, rows.Err()
}

func (s *PullReviewStore) CountApprovals(ctx context.Context, pullID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pull_reviews WHERE pull_id=$1 AND state='approved'`,
		pullID,
	).Scan(&count)
	return count, err
}

func (s *PullReviewStore) HasChangesRequested(ctx context.Context, pullID int64) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM pull_reviews WHERE pull_id=$1 AND state='changes_requested')`,
		pullID,
	).Scan(&exists)
	return exists, err
}

// RequestReview stores the reviewer in the author_id column with state 'pending'; once they submit, the state changes away from 'pending'.
func (s *PullReviewStore) ListPullIDsAwaitingReviewer(ctx context.Context, reviewerID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT pull_id FROM pull_reviews WHERE author_id = $1 AND state = 'pending'`,
		reviewerID,
	)
	if err != nil {
		return nil, fmt.Errorf("pull reviews list awaiting reviewer: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *PullReviewStore) RequestReview(ctx context.Context, pullID, repoID, reviewerID int64, reviewerName string) error {
	const q = `
INSERT INTO pull_reviews (pull_id, repo_id, author_id, author_name, state, body)
VALUES ($1, $2, $3, $4, 'pending', '')
ON CONFLICT (pull_id, author_id) DO NOTHING`
	_, err := s.db.ExecContext(ctx, q, pullID, repoID, reviewerID, reviewerName)
	return err
}

// A review that has already been submitted (state != 'pending') is left intact.
func (s *PullReviewStore) RemovePendingReview(ctx context.Context, pullID, reviewerID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM pull_reviews WHERE pull_id = $1 AND author_id = $2 AND state = 'pending'`,
		pullID, reviewerID)
	return err
}
