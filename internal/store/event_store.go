package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
)

// EventStore provides database operations for activity feed events.
type EventStore struct{ db *sql.DB }

// NewEventStore creates an EventStore backed by the given database.
func NewEventStore(db *sql.DB) *EventStore { return &EventStore{db: db} }

// Record inserts one event row.
func (s *EventStore) Record(ctx context.Context, e *model.Event) error {
	var repoID *int64
	if e.RepoID != nil {
		repoID = e.RepoID
	}
	payload := e.Payload
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO events (actor_id, actor_name, repo_id, repo_name, owner_name, event_type, payload)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		e.ActorID, e.ActorName, repoID, e.RepoName, e.OwnerName, e.EventType, payload,
	).Scan(&e.ID)
	if err != nil {
		return fmt.Errorf("event record: %w", err)
	}
	return nil
}

// ListForFeed returns paginated events for a user's personalised feed.
// Includes events from repos the user watches (non-ignoring) or owns.
func (s *EventStore) ListForFeed(ctx context.Context, userID int64, page, pageSize int) ([]model.Event, error) {
	offset := (page - 1) * pageSize
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.id, e.actor_id, e.actor_name, e.repo_id, e.repo_name, e.owner_name, e.event_type, e.payload, e.created_at
		 FROM events e
		 WHERE e.repo_id IN (
		     SELECT w.repo_id FROM watches w
		     JOIN repositories wr ON wr.id = w.repo_id
		     WHERE w.user_id = $1 AND w.level != 'ignoring'
		       AND (wr.private = false OR wr.owner_id = $1
		            OR EXISTS (SELECT 1 FROM permissions p WHERE p.repo_id = wr.id AND p.user_id = $1))
		     UNION
		     SELECT r.id FROM repositories r WHERE r.owner_id = $1
		 )
		 ORDER BY e.created_at DESC
		 LIMIT $2 OFFSET $3`,
		userID, pageSize, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("event list for feed: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

// ListByRepo returns paginated events for a single repo.
func (s *EventStore) ListByRepo(ctx context.Context, repoID int64, page, pageSize int) ([]model.Event, error) {
	offset := (page - 1) * pageSize
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, actor_id, actor_name, repo_id, repo_name, owner_name, event_type, payload, created_at
		 FROM events WHERE repo_id = $1
		 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		repoID, pageSize, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("event list by repo: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

// ListByActor returns paginated public-repo events for a single actor.
// Private-repo events are excluded so this is safe to call from public profile pages.
func (s *EventStore) ListByActor(ctx context.Context, actorID int64, page, pageSize int) ([]model.Event, error) {
	offset := (page - 1) * pageSize
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.id, e.actor_id, e.actor_name, e.repo_id, e.repo_name, e.owner_name, e.event_type, e.payload, e.created_at
		 FROM events e
		 LEFT JOIN repositories r ON r.id = e.repo_id
		 WHERE e.actor_id = $1
		   AND (r.id IS NULL OR r.private = false)
		 ORDER BY e.created_at DESC LIMIT $2 OFFSET $3`,
		actorID, pageSize, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("event list by actor: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

func scanEvents(rows *sql.Rows) ([]model.Event, error) {
	var events []model.Event
	for rows.Next() {
		var e model.Event
		var repoID sql.NullInt64
		if err := rows.Scan(&e.ID, &e.ActorID, &e.ActorName, &repoID, &e.RepoName, &e.OwnerName, &e.EventType, &e.Payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		if repoID.Valid {
			v := repoID.Int64
			e.RepoID = &v
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
