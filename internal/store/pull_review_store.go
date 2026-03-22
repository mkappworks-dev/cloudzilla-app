package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
)

type PullReviewStore struct{ db *sql.DB }

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
