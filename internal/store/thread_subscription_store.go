package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// ThreadSubscriptionStore provides database operations for per-thread notification subscriptions.
type ThreadSubscriptionStore struct{ db *sql.DB }

// NewThreadSubscriptionStore creates a ThreadSubscriptionStore backed by the given database.
func NewThreadSubscriptionStore(db *sql.DB) *ThreadSubscriptionStore {
	return &ThreadSubscriptionStore{db: db}
}

// Upsert writes state and reason, replacing any existing row for the thread.
func (s *ThreadSubscriptionStore) Upsert(ctx context.Context, userID, repoID int64, kind string, number int64, state, reason string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO thread_subscriptions (user_id, repo_id, kind, number, state, reason)
         VALUES ($1, $2, $3, $4, $5, $6)
         ON CONFLICT (user_id, repo_id, kind, number)
         DO UPDATE SET state = EXCLUDED.state, reason = EXCLUDED.reason, updated_at = NOW()`,
		userID, repoID, kind, number, state, reason,
	)
	if err != nil {
		return fmt.Errorf("thread subscription upsert: %w", err)
	}
	return nil
}

// InsertIfAbsent writes the row only when the user has none for the thread, so it never undoes a mute.
func (s *ThreadSubscriptionStore) InsertIfAbsent(ctx context.Context, userID, repoID int64, kind string, number int64, state, reason string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO thread_subscriptions (user_id, repo_id, kind, number, state, reason)
         VALUES ($1, $2, $3, $4, $5, $6)
         ON CONFLICT (user_id, repo_id, kind, number) DO NOTHING`,
		userID, repoID, kind, number, state, reason,
	)
	if err != nil {
		return fmt.Errorf("thread subscription insert if absent: %w", err)
	}
	return nil
}

// Get returns the user's row for the thread, or nil if there is none.
func (s *ThreadSubscriptionStore) Get(ctx context.Context, userID, repoID int64, kind string, number int64) (*model.ThreadSubscription, error) {
	var ts model.ThreadSubscription
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, repo_id, kind, number, state, reason, created_at, updated_at
         FROM thread_subscriptions
         WHERE user_id = $1 AND repo_id = $2 AND kind = $3 AND number = $4`,
		userID, repoID, kind, number,
	).Scan(&ts.UserID, &ts.RepoID, &ts.Kind, &ts.Number, &ts.State, &ts.Reason, &ts.CreatedAt, &ts.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("thread subscription get: %w", err)
	}
	return &ts, nil
}

// ListByThread returns the IDs of users whose row for the thread has the given state.
func (s *ThreadSubscriptionStore) ListByThread(ctx context.Context, repoID int64, kind string, number int64, state string) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT user_id FROM thread_subscriptions
         WHERE repo_id = $1 AND kind = $2 AND number = $3 AND state = $4
         ORDER BY user_id`,
		repoID, kind, number, state,
	)
	if err != nil {
		return nil, fmt.Errorf("thread subscription list: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("thread subscription list: scan user_id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
