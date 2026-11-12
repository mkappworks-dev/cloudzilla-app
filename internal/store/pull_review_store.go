package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// PullReviewStore provides database operations for pull request reviews.
type PullReviewStore struct{ db *sql.DB }

// NewPullReviewStore creates a PullReviewStore backed by the given database.
func NewPullReviewStore(db *sql.DB) *PullReviewStore { return &PullReviewStore{db: db} }

// A 'commented' review never replaces a standing verdict: the row keeps its state and body, and r comes back as stored,
// so the caller can tell the comment was not recorded.
func (s *PullReviewStore) Upsert(ctx context.Context, r *model.PullReview) error {
	const q = `
INSERT INTO pull_reviews (pull_id, repo_id, author_id, author_name, state, body, submitted_at)
VALUES ($1, $2, $3, $4, $5, $6, NOW())
ON CONFLICT (pull_id, author_id) WHERE author_id <> ghost_user_id() DO UPDATE
  SET state = CASE WHEN $5 = 'commented' AND pull_reviews.state IN ('approved', 'changes_requested')
                   THEN pull_reviews.state ELSE $5 END,
      body = CASE WHEN $5 = 'commented' AND pull_reviews.state IN ('approved', 'changes_requested')
                  THEN pull_reviews.body ELSE $6 END,
      submitted_at = CASE WHEN $5 = 'commented' AND pull_reviews.state IN ('approved', 'changes_requested')
                          THEN pull_reviews.submitted_at ELSE NOW() END,
      dismissed_by_name = '', dismissed_reason = '', dismissed_at = NULL,
      updated_at = NOW()
RETURNING id, state, body, submitted_at, created_at, updated_at`
	return s.db.QueryRowContext(ctx, q,
		r.PullID, r.RepoID, r.AuthorID, r.AuthorName, r.State, r.Body,
	).Scan(&r.ID, &r.State, &r.Body, &r.SubmittedAt, &r.CreatedAt, &r.UpdatedAt)
}

func scanPullReview(row interface{ Scan(...any) error }) (model.PullReview, error) {
	var r model.PullReview
	var submittedAt, dismissedAt sql.NullTime
	if err := row.Scan(
		&r.ID, &r.PullID, &r.RepoID, &r.AuthorID, &r.AuthorName,
		&r.State, &r.Body, &submittedAt,
		&r.DismissedBy, &r.DismissedReason, &dismissedAt, &r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		return r, err
	}
	if submittedAt.Valid {
		r.SubmittedAt = &submittedAt.Time
	}
	if dismissedAt.Valid {
		r.DismissedAt = &dismissedAt.Time
	}
	return r, nil
}

func (s *PullReviewStore) ListByPull(ctx context.Context, pullID int64) ([]model.PullReview, error) {
	const q = `
SELECT id, pull_id, repo_id, author_id, author_name, state, body, submitted_at,
       dismissed_by_name, dismissed_reason, dismissed_at, created_at, updated_at
FROM pull_reviews WHERE pull_id=$1 ORDER BY created_at ASC`
	rows, err := s.db.QueryContext(ctx, q, pullID)
	if err != nil {
		return nil, fmt.Errorf("pull reviews list: %w", err)
	}
	defer rows.Close()
	var reviews []model.PullReview
	for rows.Next() {
		r, err := scanPullReview(rows)
		if err != nil {
			return nil, err
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
		r, err := scanPullReview(rows)
		if err != nil {
			return nil, err
		}
		result[r.PullID] = append(result[r.PullID], r)
	}
	return result, rows.Err()
}

// The ghost's reviews count toward neither gate: a deleted account's approval
// is no longer backed by anyone, and nobody can withdraw its request for changes.
func (s *PullReviewStore) CountApprovals(ctx context.Context, pullID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pull_reviews WHERE pull_id=$1 AND state='approved' AND author_id <> ghost_user_id()`,
		pullID,
	).Scan(&count)
	return count, err
}

func (s *PullReviewStore) HasChangesRequested(ctx context.Context, pullID int64) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM pull_reviews WHERE pull_id=$1 AND state='changes_requested' AND author_id <> ghost_user_id())`,
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

// ListPendingReviewsForReviewer maps pull_id → created_at; MIN guards against future duplicate rows.
func (s *PullReviewStore) ListPendingReviewsForReviewer(ctx context.Context, reviewerID int64) (map[int64]time.Time, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT pull_id, MIN(created_at) FROM pull_reviews
		 WHERE author_id = $1 AND state = 'pending'
		 GROUP BY pull_id`,
		reviewerID,
	)
	if err != nil {
		return nil, fmt.Errorf("pull reviews pending for reviewer: %w", err)
	}
	defer rows.Close()
	m := make(map[int64]time.Time)
	for rows.Next() {
		var pullID int64
		var requestedAt time.Time
		if err := rows.Scan(&pullID, &requestedAt); err != nil {
			return nil, err
		}
		m[pullID] = requestedAt
	}
	return m, rows.Err()
}

func (s *PullReviewStore) RequestReview(ctx context.Context, pullID, repoID, reviewerID int64, reviewerName string) error {
	const q = `
INSERT INTO pull_reviews (pull_id, repo_id, author_id, author_name, state, body)
VALUES ($1, $2, $3, $4, 'pending', '')
ON CONFLICT (pull_id, author_id) WHERE author_id <> ghost_user_id() DO NOTHING`
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

// GetInPull returns sql.ErrNoRows when reviewID belongs to another pull.
func (s *PullReviewStore) GetInPull(ctx context.Context, pullID, reviewID int64) (*model.PullReview, error) {
	r, err := scanPullReview(s.db.QueryRowContext(ctx, `
SELECT id, pull_id, repo_id, author_id, author_name, state, body, submitted_at,
       dismissed_by_name, dismissed_reason, dismissed_at, created_at, updated_at
FROM pull_reviews WHERE id = $1 AND pull_id = $2`, reviewID, pullID))
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// Dismiss reports false when the review no longer holds a request for changes, so two admins dismissing at once
// record one dismissal.
func (s *PullReviewStore) Dismiss(ctx context.Context, pullID, reviewID int64, byName, reason string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE pull_reviews
SET state = 'dismissed', dismissed_by_name = $3, dismissed_reason = $4, dismissed_at = NOW(), updated_at = NOW()
WHERE id = $1 AND pull_id = $2 AND state = 'changes_requested'`, reviewID, pullID, byName, reason)
	if err != nil {
		return false, fmt.Errorf("dismiss pull review: %w", err)
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
