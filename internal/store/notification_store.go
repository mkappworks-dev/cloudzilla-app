package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
)

type NotificationStore struct{ db *sql.DB }

func NewNotificationStore(db *sql.DB) *NotificationStore { return &NotificationStore{db: db} }

func (s *NotificationStore) Create(ctx context.Context, n *model.Notification) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO notifications (user_id, actor_id, actor_name, type, repo_id, repo_name, owner_name, subject_id, subject_url)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		n.UserID, n.ActorID, n.ActorName, string(n.Type), n.RepoID, n.RepoName, n.OwnerName, n.SubjectID, n.SubjectURL,
	).Scan(&n.ID)
	if err != nil {
		return fmt.Errorf("notification create: %w", err)
	}
	return nil
}

func (s *NotificationStore) ListByUser(ctx context.Context, userID int64) ([]model.Notification, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, actor_id, actor_name, type, repo_id, repo_name, owner_name, subject_id, subject_url, read, created_at
		 FROM notifications WHERE user_id = $1 ORDER BY created_at DESC LIMIT 50`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("notification list by user: %w", err)
	}
	defer rows.Close()
	var notifs []model.Notification
	for rows.Next() {
		var n model.Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.ActorID, &n.ActorName, &n.Type, &n.RepoID, &n.RepoName, &n.OwnerName, &n.SubjectID, &n.SubjectURL, &n.Read, &n.CreatedAt); err != nil {
			return nil, err
		}
		notifs = append(notifs, n)
	}
	return notifs, rows.Err()
}

func (s *NotificationStore) CountUnread(ctx context.Context, userID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND read = FALSE`,
		userID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("notification count unread: %w", err)
	}
	return count, nil
}

func (s *NotificationStore) MarkRead(ctx context.Context, id, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET read = TRUE WHERE id = $1 AND user_id = $2`,
		id, userID,
	)
	return err
}

func (s *NotificationStore) ListUnreadByUser(ctx context.Context, userID int64) ([]model.Notification, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, actor_id, actor_name, type, repo_id, repo_name, owner_name, subject_id, subject_url, read, created_at
		 FROM notifications WHERE user_id = $1 AND read = FALSE ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("notification list unread by user: %w", err)
	}
	defer rows.Close()
	var notifs []model.Notification
	for rows.Next() {
		var n model.Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.ActorID, &n.ActorName, &n.Type, &n.RepoID, &n.RepoName, &n.OwnerName, &n.SubjectID, &n.SubjectURL, &n.Read, &n.CreatedAt); err != nil {
			return nil, err
		}
		notifs = append(notifs, n)
	}
	return notifs, rows.Err()
}

func (s *NotificationStore) MarkAllRead(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET read = TRUE WHERE user_id = $1 AND read = FALSE`,
		userID,
	)
	return err
}
