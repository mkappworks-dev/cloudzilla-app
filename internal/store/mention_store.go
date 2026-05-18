package store

import (
	"context"
	"database/sql"
)

// MentionStore provides database operations for @mention records.
type MentionStore struct{ db *sql.DB }

// NewMentionStore creates a MentionStore backed by the given database.
func NewMentionStore(db *sql.DB) *MentionStore { return &MentionStore{db: db} }

// Create inserts a single mention row. Ignores conflicts (duplicate mention).
func (s *MentionStore) Create(ctx context.Context, commentID, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mentions (comment_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		commentID, userID,
	)
	return err
}

// CreateBatch inserts mention rows for all userIDs in one round-trip.
// No-op if userIDs is empty.
func (s *MentionStore) CreateBatch(ctx context.Context, commentID int64, userIDs []int64) error {
	if len(userIDs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO mentions (comment_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, uid := range userIDs {
		if _, err := stmt.ExecContext(ctx, commentID, uid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *MentionStore) ListPullIDsMentioning(ctx context.Context, userID int64) ([]int64, error) {
	return s.listMentionTargetIDs(ctx, userID, "c.pull_id")
}

func (s *MentionStore) ListIssueIDsMentioning(ctx context.Context, userID int64) ([]int64, error) {
	return s.listMentionTargetIDs(ctx, userID, "c.issue_id")
}

// listMentionTargetIDs returns distinct, non-null values of col (a fixed
// comments-table column, never user input) for comments that mention userID.
func (s *MentionStore) listMentionTargetIDs(ctx context.Context, userID int64, col string) ([]int64, error) {
	q := `SELECT DISTINCT ` + col + `
	      FROM mentions m
	      JOIN comments c ON c.id = m.comment_id
	      WHERE m.user_id = $1 AND ` + col + ` IS NOT NULL`
	rows, err := s.db.QueryContext(ctx, q, userID)
	if err != nil {
		return nil, err
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
