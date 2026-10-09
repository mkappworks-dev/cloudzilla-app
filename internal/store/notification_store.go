package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

const notificationCols = `id, user_id, actor_id, actor_name, type, repo_id, repo_name, owner_name, subject_id, subject_url, subject_title, read, created_at`

// Inbox filters for ListPage and CountByFilter.
const (
	NotifFilterInbox  = "inbox"
	NotifFilterUnread = "unread"
	NotifFilterRead   = "read"
)

// filterClause returns a fixed SQL fragment, never one built from input; an unknown filter means inbox.
func filterClause(filter string) string {
	switch filter {
	case NotifFilterUnread:
		return " AND read = FALSE"
	case NotifFilterRead:
		return " AND read = TRUE"
	}
	return ""
}

// NotificationStore provides database operations for in-app notifications.
type NotificationStore struct{ db *sql.DB }

// NewNotificationStore creates a NotificationStore backed by the given database.
func NewNotificationStore(db *sql.DB) *NotificationStore { return &NotificationStore{db: db} }

func (s *NotificationStore) Create(ctx context.Context, n *model.Notification) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO notifications (user_id, actor_id, actor_name, type, repo_id, repo_name, owner_name, subject_id, subject_url, subject_title)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
		n.UserID, n.ActorID, n.ActorName, string(n.Type), n.RepoID, n.RepoName, n.OwnerName, n.SubjectID, n.SubjectURL, n.SubjectTitle,
	).Scan(&n.ID)
	if err != nil {
		return fmt.Errorf("notification create: %w", err)
	}
	return nil
}

func (s *NotificationStore) ListByUser(ctx context.Context, userID int64) ([]model.Notification, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+notificationCols+`
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
		if err := rows.Scan(&n.ID, &n.UserID, &n.ActorID, &n.ActorName, &n.Type, &n.RepoID, &n.RepoName, &n.OwnerName, &n.SubjectID, &n.SubjectURL, &n.SubjectTitle, &n.Read, &n.CreatedAt); err != nil {
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

// ListUnreadReadable returns userID's unread notifications on repos the user
// can still read; access may have been revoked since a notification was created.
// A repository transfer still offered to the user counts as readable, since
// its repo isn't until they accept.
func (s *NotificationStore) ListUnreadReadable(ctx context.Context, userID int64) ([]model.Notification, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT n.id, n.user_id, n.actor_id, n.actor_name, n.type, n.repo_id, n.repo_name, n.owner_name, n.subject_id, n.subject_url, n.subject_title, n.read, n.created_at
		 FROM notifications n JOIN repositories r ON r.id = n.repo_id
		 WHERE n.user_id = $1 AND n.read = FALSE AND (`+readableBy("r", "$1")+`
		    OR n.type = 'repo_transfer' AND EXISTS (SELECT 1 FROM repo_transfers t
		       WHERE t.id = n.subject_id AND t.recipient_id = n.user_id AND t.expires_at > NOW()))
		 ORDER BY n.created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("notification list unread readable: %w", err)
	}
	defer rows.Close()
	var notifs []model.Notification
	for rows.Next() {
		var n model.Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.ActorID, &n.ActorName, &n.Type, &n.RepoID, &n.RepoName, &n.OwnerName, &n.SubjectID, &n.SubjectURL, &n.SubjectTitle, &n.Read, &n.CreatedAt); err != nil {
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

// ListPage returns one page of userID's notifications, newest first. The id tiebreaker keeps
// rows that share a timestamp from straddling or repeating across pages.
func (s *NotificationStore) ListPage(ctx context.Context, userID int64, filter string, limit, offset int) ([]model.Notification, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+notificationCols+` FROM notifications WHERE user_id = $1`+filterClause(filter)+`
		 ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`,
		userID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("notification list page: %w", err)
	}
	defer rows.Close()
	var notifs []model.Notification
	for rows.Next() {
		var n model.Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.ActorID, &n.ActorName, &n.Type, &n.RepoID, &n.RepoName, &n.OwnerName, &n.SubjectID, &n.SubjectURL, &n.SubjectTitle, &n.Read, &n.CreatedAt); err != nil {
			return nil, err
		}
		notifs = append(notifs, n)
	}
	return notifs, rows.Err()
}

func (s *NotificationStore) CountByFilter(ctx context.Context, userID int64, filter string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notifications WHERE user_id = $1`+filterClause(filter),
		userID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("notification count by filter: %w", err)
	}
	return count, nil
}
