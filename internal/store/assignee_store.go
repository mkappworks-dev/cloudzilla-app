package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// AssigneeStore provides database operations for issue and PR assignees.
type AssigneeStore struct{ db *sql.DB }

// NewAssigneeStore creates an AssigneeStore backed by the given database.
func NewAssigneeStore(db *sql.DB) *AssigneeStore { return &AssigneeStore{db: db} }

func (s *AssigneeStore) AddToIssue(ctx context.Context, issueID, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO issue_assignees (issue_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		issueID, userID,
	)
	return err
}

func (s *AssigneeStore) RemoveFromIssue(ctx context.Context, issueID, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM issue_assignees WHERE issue_id = $1 AND user_id = $2`,
		issueID, userID,
	)
	return err
}

func (s *AssigneeStore) ListByIssue(ctx context.Context, issueID int64) ([]model.User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT u.id, u.username, u.email, u.bio, u.avatar_url, u.is_superadmin, u.is_invited, u.created_at, u.updated_at
		 FROM users u JOIN issue_assignees ia ON u.id = ia.user_id
		 WHERE ia.issue_id = $1 ORDER BY u.username`,
		issueID,
	)
	if err != nil {
		return nil, fmt.Errorf("assignee list by issue: %w", err)
	}
	defer rows.Close()
	return scanUsers(rows)
}

func (s *AssigneeStore) AddToPull(ctx context.Context, pullID, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pull_assignees (pull_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		pullID, userID,
	)
	return err
}

func (s *AssigneeStore) RemoveFromPull(ctx context.Context, pullID, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM pull_assignees WHERE pull_id = $1 AND user_id = $2`,
		pullID, userID,
	)
	return err
}

func (s *AssigneeStore) ListByPull(ctx context.Context, pullID int64) ([]model.User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT u.id, u.username, u.email, u.bio, u.avatar_url, u.is_superadmin, u.is_invited, u.created_at, u.updated_at
		 FROM users u JOIN pull_assignees pa ON u.id = pa.user_id
		 WHERE pa.pull_id = $1 ORDER BY u.username`,
		pullID,
	)
	if err != nil {
		return nil, fmt.Errorf("assignee list by pull: %w", err)
	}
	defer rows.Close()
	return scanUsers(rows)
}

func (s *AssigneeStore) ListByPullIDs(ctx context.Context, pullIDs []int64) (map[int64][]model.User, error) {
	if len(pullIDs) == 0 {
		return map[int64][]model.User{}, nil
	}
	placeholders := make([]string, len(pullIDs))
	args := make([]any, len(pullIDs))
	for i, id := range pullIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	q := fmt.Sprintf(
		`SELECT pa.pull_id, u.id, u.username, u.email, u.bio, u.avatar_url, u.is_superadmin, u.is_invited, u.created_at, u.updated_at
		 FROM users u JOIN pull_assignees pa ON u.id = pa.user_id
		 WHERE pa.pull_id IN (%s) ORDER BY u.username`,
		strings.Join(placeholders, ","),
	)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("assignee list by pull ids: %w", err)
	}
	defer rows.Close()
	result := make(map[int64][]model.User)
	for rows.Next() {
		var pullID int64
		var u model.User
		if err := rows.Scan(&pullID, &u.ID, &u.Username, &u.Email, &u.Bio, &u.AvatarURL, &u.IsSuperadmin, &u.IsInvited, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		result[pullID] = append(result[pullID], u)
	}
	return result, rows.Err()
}

func scanUsers(rows *sql.Rows) ([]model.User, error) {
	var users []model.User
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.Bio, &u.AvatarURL, &u.IsSuperadmin, &u.IsInvited, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}
