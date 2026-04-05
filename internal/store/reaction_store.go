package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
)

// ReactionStore provides database operations for comment reactions.
type ReactionStore struct{ db *sql.DB }

// NewReactionStore creates a ReactionStore backed by the given database.
func NewReactionStore(db *sql.DB) *ReactionStore { return &ReactionStore{db: db} }

// Toggle adds the reaction if it does not exist, or removes it if it does.
// Returns true if the reaction was added, false if removed.
func (s *ReactionStore) Toggle(ctx context.Context, userID, commentID int64, emoji string) (bool, error) {
	var existing int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM reactions WHERE user_id=$1 AND comment_id=$2 AND emoji=$3`,
		userID, commentID, emoji,
	).Scan(&existing)

	if err == sql.ErrNoRows {
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO reactions (user_id, comment_id, emoji) VALUES ($1, $2, $3)`,
			userID, commentID, emoji,
		)
		if err != nil {
			return false, fmt.Errorf("reaction insert: %w", err)
		}
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("reaction lookup: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`DELETE FROM reactions WHERE user_id=$1 AND comment_id=$2 AND emoji=$3`,
		userID, commentID, emoji,
	)
	if err != nil {
		return false, fmt.Errorf("reaction delete: %w", err)
	}
	return false, nil
}

// CommentBelongsToRepo returns true if the comment with the given id belongs to the given repo.
func (s *ReactionStore) CommentBelongsToRepo(ctx context.Context, commentID, repoID int64) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM comments WHERE id=$1 AND repo_id=$2)`,
		commentID, repoID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("comment belongs to repo: %w", err)
	}
	return exists, nil
}

// ListByComment returns one ReactionSummary per emoji that has at least one reaction on commentID.
// callerID=0 means anonymous — UserReacted will always be false.
func (s *ReactionStore) ListByComment(ctx context.Context, commentID, callerID int64) ([]model.ReactionSummary, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT emoji, COUNT(*) AS count, BOOL_OR(user_id = $2) AS user_reacted
		 FROM reactions
		 WHERE comment_id = $1
		 GROUP BY emoji
		 ORDER BY emoji`,
		commentID, callerID,
	)
	if err != nil {
		return nil, fmt.Errorf("reaction list: %w", err)
	}
	defer rows.Close()
	var out []model.ReactionSummary
	for rows.Next() {
		var r model.ReactionSummary
		if err := rows.Scan(&r.Emoji, &r.Count, &r.UserReacted); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
