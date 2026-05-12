package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// SavedReplyStore provides database operations for user-defined saved reply templates.
type SavedReplyStore struct{ db *sql.DB }

// NewSavedReplyStore creates a SavedReplyStore backed by the given database.
func NewSavedReplyStore(db *sql.DB) *SavedReplyStore { return &SavedReplyStore{db: db} }

func (s *SavedReplyStore) Create(ctx context.Context, r *model.SavedReply) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO saved_replies (user_id, title, body)
		 VALUES ($1, $2, $3)
		 RETURNING id, created_at, updated_at`,
		r.UserID, r.Title, r.Body,
	).Scan(&r.ID, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return fmt.Errorf("saved_reply create: %w", err)
	}
	return nil
}

func (s *SavedReplyStore) ListByUser(ctx context.Context, userID int64) ([]model.SavedReply, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, title, body, created_at, updated_at
		 FROM saved_replies WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("saved_reply list: %w", err)
	}
	defer rows.Close()
	var replies []model.SavedReply
	for rows.Next() {
		var r model.SavedReply
		if err := rows.Scan(&r.ID, &r.UserID, &r.Title, &r.Body, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		replies = append(replies, r)
	}
	return replies, rows.Err()
}

func (s *SavedReplyStore) GetByID(ctx context.Context, id int64) (*model.SavedReply, error) {
	r := &model.SavedReply{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, title, body, created_at, updated_at
		 FROM saved_replies WHERE id = $1`,
		id,
	).Scan(&r.ID, &r.UserID, &r.Title, &r.Body, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("saved_reply get: %w", err)
	}
	return r, nil
}

func (s *SavedReplyStore) Update(ctx context.Context, r *model.SavedReply) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE saved_replies SET title=$1, body=$2, updated_at=NOW() WHERE id=$3 AND user_id=$4`,
		r.Title, r.Body, r.ID, r.UserID,
	)
	return err
}

func (s *SavedReplyStore) Delete(ctx context.Context, id, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM saved_replies WHERE id=$1 AND user_id=$2`,
		id, userID,
	)
	return err
}
