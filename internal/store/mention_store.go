package store

import (
	"context"
	"database/sql"
)

type MentionStore struct{ db *sql.DB }

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
