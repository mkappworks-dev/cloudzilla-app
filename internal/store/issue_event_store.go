package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

type IssueEventStore struct{ db *sql.DB }

func NewIssueEventStore(db *sql.DB) *IssueEventStore { return &IssueEventStore{db: db} }

// ClaimClose closes an open issue and records e, its closed event, in one
// transaction. It reports false, changing nothing, when the issue isn't open or
// e's PR or commit already closed it once.
func (s *IssueEventStore) ClaimClose(ctx context.Context, e *model.IssueEvent) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("issue close claim begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM issues WHERE id = $1 FOR UPDATE`, e.IssueID).Scan(&state); err != nil {
		return false, fmt.Errorf("issue close claim lock: %w", err)
	}
	if state != string(model.IssueStateOpen) {
		return false, nil
	}
	e.Type = model.IssueEventClosed
	err = tx.QueryRowContext(ctx,
		`INSERT INTO issue_events (issue_id, actor_id, actor_name, event_type, pull_id, commit_sha, source_repo_id)
		 VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7)
		 ON CONFLICT DO NOTHING
		 RETURNING id, created_at`,
		e.IssueID, e.ActorID, e.ActorName, e.Type, e.PullID, e.CommitSHA, e.SourceRepoID,
	).Scan(&e.ID, &e.CreatedAt)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("issue close claim event: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE issues SET state = 'closed', closed_at = $2, updated_at = $2 WHERE id = $1`,
		e.IssueID, e.CreatedAt,
	); err != nil {
		return false, fmt.Errorf("issue close claim update: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("issue close claim commit: %w", err)
	}
	return true, nil
}

// SetState opens or closes an issue by hand, recording e as its event, in one
// transaction. It reports false, recording nothing, when the issue is already
// in that state.
func (s *IssueEventStore) SetState(ctx context.Context, state model.IssueState, e *model.IssueEvent) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("issue set state begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var cur string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM issues WHERE id = $1 FOR UPDATE`, e.IssueID).Scan(&cur); err != nil {
		return false, fmt.Errorf("issue set state lock: %w", err)
	}
	if cur == string(state) {
		return false, nil
	}
	e.Type = model.IssueEventClosed
	closedAt := "NOW()"
	if state == model.IssueStateOpen {
		e.Type, closedAt = model.IssueEventReopened, "NULL"
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE issues SET state = $2, closed_at = `+closedAt+`, updated_at = NOW() WHERE id = $1`, e.IssueID, string(state),
	); err != nil {
		return false, fmt.Errorf("issue set state update: %w", err)
	}
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO issue_events (issue_id, actor_id, actor_name, event_type) VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
		e.IssueID, e.ActorID, e.ActorName, e.Type,
	).Scan(&e.ID, &e.CreatedAt); err != nil {
		return false, fmt.Errorf("issue set state event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("issue set state commit: %w", err)
	}
	return true, nil
}

// ListByIssue returns an issue's events, oldest first. The PR or commit behind a
// close is left out when viewer can't read the repo it came from.
func (s *IssueEventStore) ListByIssue(ctx context.Context, issueID int64, viewer *int64) ([]model.IssueEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.id, e.issue_id, e.actor_id, e.actor_name, e.event_type,
		        CASE WHEN r.id IS NULL THEN NULL ELSE e.pull_id END,
		        CASE WHEN r.id IS NULL THEN '' ELSE COALESCE(e.commit_sha, '') END,
		        r.id, e.created_at,
		        COALESCE(p.number, 0), COALESCE(r.owner_name, ''), COALESCE(r.name, '')
		 FROM issue_events e
		 LEFT JOIN repositories r ON r.id = e.source_repo_id AND r.deleted_at IS NULL AND `+readableBy("r", "$2")+`
		 LEFT JOIN pull_requests p ON p.id = e.pull_id AND r.id IS NOT NULL
		 WHERE e.issue_id = $1
		 ORDER BY e.created_at, e.id`,
		issueID, viewerID(viewer),
	)
	if err != nil {
		return nil, fmt.Errorf("issue events list: %w", err)
	}
	defer rows.Close()
	var out []model.IssueEvent
	for rows.Next() {
		var e model.IssueEvent
		var pullID, sourceRepoID sql.NullInt64
		if err := rows.Scan(&e.ID, &e.IssueID, &e.ActorID, &e.ActorName, &e.Type, &pullID,
			&e.CommitSHA, &sourceRepoID, &e.CreatedAt,
			&e.PullNumber, &e.SourceOwner, &e.SourceRepo); err != nil {
			return nil, fmt.Errorf("issue events scan: %w", err)
		}
		if pullID.Valid {
			e.PullID = &pullID.Int64
		}
		if sourceRepoID.Valid {
			e.SourceRepoID = &sourceRepoID.Int64
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
