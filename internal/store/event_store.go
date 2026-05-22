package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
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

// ListForFeed returns paginated events for a user's personalised feed:
// events from repos the user watches (non-ignoring) or owns, plus events on
// issues and pull requests the user is involved in (author, assignee,
// requested reviewer, or mentioned).
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
		 OR e.id IN (
		     SELECT ie.id FROM events ie
		     JOIN issues i ON i.repo_id = ie.repo_id AND i.number = (ie.payload->>'number')::int
		     WHERE (ie.event_type IN ('issue_opened', 'issue_closed')
		            OR (ie.event_type = 'comment' AND ie.payload->>'kind' = 'issue'))
		       AND (i.author_id = $1
		            OR EXISTS (SELECT 1 FROM issue_assignees ia WHERE ia.issue_id = i.id AND ia.user_id = $1)
		            OR EXISTS (SELECT 1 FROM mentions m JOIN comments c ON c.id = m.comment_id
		                       WHERE c.issue_id = i.id AND m.user_id = $1))
		 )
		 OR e.id IN (
		     SELECT pe.id FROM events pe
		     JOIN pull_requests p ON p.repo_id = pe.repo_id AND p.number = (pe.payload->>'number')::int
		     WHERE (pe.event_type IN ('pr_opened', 'pr_merged', 'pr_closed')
		            OR (pe.event_type = 'comment' AND pe.payload->>'kind' = 'pull'))
		       AND (p.author_id = $1
		            OR EXISTS (SELECT 1 FROM pull_assignees pa WHERE pa.pull_id = p.id AND pa.user_id = $1)
		            OR EXISTS (SELECT 1 FROM pull_reviews prv WHERE prv.pull_id = p.id AND prv.author_id = $1)
		            OR EXISTS (SELECT 1 FROM mentions m JOIN comments c ON c.id = m.comment_id
		                       WHERE c.pull_id = p.id AND m.user_id = $1))
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

// ListWatching returns paginated events from repos the user watches (non-ignoring),
// excluding private repos the user cannot access.
func (s *EventStore) ListWatching(ctx context.Context, userID int64, page, pageSize int) ([]model.Event, error) {
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
		 )
		 ORDER BY e.created_at DESC
		 LIMIT $2 OFFSET $3`,
		userID, pageSize, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("event list watching: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

// ListOwnActivity returns paginated events the user performed, across all repos.
func (s *EventStore) ListOwnActivity(ctx context.Context, userID int64, page, pageSize int) ([]model.Event, error) {
	offset := (page - 1) * pageSize
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, actor_id, actor_name, repo_id, repo_name, owner_name, event_type, payload, created_at
		 FROM events WHERE actor_id = $1
		 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		userID, pageSize, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("event list own activity: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

// FeedCounts returns the total event count for each activity-feed scope in a
// single round-trip, keyed "all", "yours", and "watching". The "all" and
// "watching" predicates mirror ListForFeed and ListWatching respectively.
func (s *EventStore) FeedCounts(ctx context.Context, userID int64) (map[string]int, error) {
	var all, yours, watching int
	err := s.db.QueryRowContext(ctx,
		`SELECT
		   (SELECT COUNT(*) FROM events e
		    WHERE e.repo_id IN (
		        SELECT w.repo_id FROM watches w
		        JOIN repositories wr ON wr.id = w.repo_id
		        WHERE w.user_id = $1 AND w.level != 'ignoring'
		          AND (wr.private = false OR wr.owner_id = $1
		               OR EXISTS (SELECT 1 FROM permissions p WHERE p.repo_id = wr.id AND p.user_id = $1))
		        UNION
		        SELECT r.id FROM repositories r WHERE r.owner_id = $1
		    )
		    OR e.id IN (
		        SELECT ie.id FROM events ie
		        JOIN issues i ON i.repo_id = ie.repo_id AND i.number = (ie.payload->>'number')::int
		        WHERE (ie.event_type IN ('issue_opened', 'issue_closed')
		               OR (ie.event_type = 'comment' AND ie.payload->>'kind' = 'issue'))
		          AND (i.author_id = $1
		               OR EXISTS (SELECT 1 FROM issue_assignees ia WHERE ia.issue_id = i.id AND ia.user_id = $1)
		               OR EXISTS (SELECT 1 FROM mentions m JOIN comments c ON c.id = m.comment_id
		                          WHERE c.issue_id = i.id AND m.user_id = $1))
		    )
		    OR e.id IN (
		        SELECT pe.id FROM events pe
		        JOIN pull_requests p ON p.repo_id = pe.repo_id AND p.number = (pe.payload->>'number')::int
		        WHERE (pe.event_type IN ('pr_opened', 'pr_merged', 'pr_closed')
		               OR (pe.event_type = 'comment' AND pe.payload->>'kind' = 'pull'))
		          AND (p.author_id = $1
		               OR EXISTS (SELECT 1 FROM pull_assignees pa WHERE pa.pull_id = p.id AND pa.user_id = $1)
		               OR EXISTS (SELECT 1 FROM pull_reviews prv WHERE prv.pull_id = p.id AND prv.author_id = $1)
		               OR EXISTS (SELECT 1 FROM mentions m JOIN comments c ON c.id = m.comment_id
		                          WHERE c.pull_id = p.id AND m.user_id = $1))
		    )),
		   (SELECT COUNT(*) FROM events WHERE actor_id = $1),
		   (SELECT COUNT(*) FROM events e
		    WHERE e.repo_id IN (
		        SELECT w.repo_id FROM watches w
		        JOIN repositories wr ON wr.id = w.repo_id
		        WHERE w.user_id = $1 AND w.level != 'ignoring'
		          AND (wr.private = false OR wr.owner_id = $1
		               OR EXISTS (SELECT 1 FROM permissions p WHERE p.repo_id = wr.id AND p.user_id = $1))
		    ))`,
		userID,
	).Scan(&all, &yours, &watching)
	if err != nil {
		return nil, fmt.Errorf("event feed counts: %w", err)
	}
	return map[string]int{"all": all, "yours": yours, "watching": watching}, nil
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
