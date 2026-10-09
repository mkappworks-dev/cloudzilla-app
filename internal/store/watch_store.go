package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// WatchStore provides database operations for repository watch subscriptions.
type WatchStore struct{ db *sql.DB }

// NewWatchStore creates a WatchStore backed by the given database.
func NewWatchStore(db *sql.DB) *WatchStore { return &WatchStore{db: db} }

// Set upserts the watch level for a user/repo pair.
func (s *WatchStore) Set(ctx context.Context, userID, repoID int64, level string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO watches (user_id, repo_id, level)
         VALUES ($1, $2, $3)
         ON CONFLICT (user_id, repo_id) DO UPDATE SET level = EXCLUDED.level`,
		userID, repoID, level,
	)
	return err
}

// Delete removes the watch row for a user/repo pair.
func (s *WatchStore) Delete(ctx context.Context, userID, repoID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM watches WHERE user_id = $1 AND repo_id = $2`,
		userID, repoID,
	)
	return err
}

// Get returns the watch row for a user/repo pair, or nil if not found.
func (s *WatchStore) Get(ctx context.Context, userID, repoID int64) (*model.Watch, error) {
	var w model.Watch
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, repo_id, level, created_at
         FROM watches WHERE user_id = $1 AND repo_id = $2`,
		userID, repoID,
	).Scan(&w.ID, &w.UserID, &w.RepoID, &w.Level, &w.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("watch get: %w", err)
	}
	return &w, nil
}

// ListWatchersByRepo returns user IDs of all watchers for a repo.
// If level is empty, returns all non-ignoring watchers.
// If level is a specific value, returns only watchers at that level.
func (s *WatchStore) ListWatchersByRepo(ctx context.Context, repoID int64, level string) ([]int64, error) {
	var rows *sql.Rows
	var err error
	if level == "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT user_id FROM watches WHERE repo_id = $1 AND level != 'ignoring'`,
			repoID,
		)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT user_id FROM watches WHERE repo_id = $1 AND level = $2`,
			repoID, level,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("watch list watchers: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("watch list watchers: scan user_id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
