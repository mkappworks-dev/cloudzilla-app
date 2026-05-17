package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// PullEventStore persists pull request timeline events.
type PullEventStore struct{ db *sql.DB }

// NewPullEventStore creates a PullEventStore backed by the given database.
func NewPullEventStore(db *sql.DB) *PullEventStore { return &PullEventStore{db: db} }

// Create inserts a timeline event and back-fills its ID and CreatedAt.
func (s *PullEventStore) Create(ctx context.Context, e *model.PullEvent) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO pull_events (pull_id, actor_id, actor_name, event_type, detail)
         VALUES ($1, $2, $3, $4, $5)
         RETURNING id, created_at`,
		e.PullID, e.ActorID, e.ActorName, e.Type, e.Detail,
	).Scan(&e.ID, &e.CreatedAt)
}

// ListByPull returns a pull request's timeline events oldest-first.
func (s *PullEventStore) ListByPull(ctx context.Context, pullID int64) ([]model.PullEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, pull_id, actor_id, actor_name, event_type, detail, created_at
         FROM pull_events WHERE pull_id = $1 ORDER BY created_at`,
		pullID,
	)
	if err != nil {
		return nil, fmt.Errorf("pull events list: %w", err)
	}
	defer rows.Close()
	var out []model.PullEvent
	for rows.Next() {
		var e model.PullEvent
		if err := rows.Scan(&e.ID, &e.PullID, &e.ActorID, &e.ActorName, &e.Type, &e.Detail, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("pull events scan: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
