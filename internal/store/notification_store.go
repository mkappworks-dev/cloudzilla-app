package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

const notificationCols = `id, user_id, actor_id, actor_name, type, repo_id, repo_name, owner_name, subject_id, subject_url, subject_title, COALESCE(subject_kind, ''), read, created_at`

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
		`INSERT INTO notifications (user_id, actor_id, actor_name, type, repo_id, repo_name, owner_name, subject_id, subject_url, subject_title, subject_kind)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULLIF($11, '')) RETURNING id`,
		n.UserID, n.ActorID, n.ActorName, string(n.Type), n.RepoID, n.RepoName, n.OwnerName, n.SubjectID, n.SubjectURL, n.SubjectTitle, n.SubjectKind,
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
		if err := rows.Scan(&n.ID, &n.UserID, &n.ActorID, &n.ActorName, &n.Type, &n.RepoID, &n.RepoName, &n.OwnerName, &n.SubjectID, &n.SubjectURL, &n.SubjectTitle, &n.SubjectKind, &n.Read, &n.CreatedAt); err != nil {
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
		`SELECT n.id, n.user_id, n.actor_id, n.actor_name, n.type, n.repo_id, n.repo_name, n.owner_name, n.subject_id, n.subject_url, n.subject_title, COALESCE(n.subject_kind, ''), n.read, n.created_at
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
		if err := rows.Scan(&n.ID, &n.UserID, &n.ActorID, &n.ActorName, &n.Type, &n.RepoID, &n.RepoName, &n.OwnerName, &n.SubjectID, &n.SubjectURL, &n.SubjectTitle, &n.SubjectKind, &n.Read, &n.CreatedAt); err != nil {
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
		if err := rows.Scan(&n.ID, &n.UserID, &n.ActorID, &n.ActorName, &n.Type, &n.RepoID, &n.RepoName, &n.OwnerName, &n.SubjectID, &n.SubjectURL, &n.SubjectTitle, &n.SubjectKind, &n.Read, &n.CreatedAt); err != nil {
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

// inPlaceholders returns "$start,$start+1,..." for ids and the ids as query args.
func inPlaceholders(start int, ids []int64) (string, []any) {
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		ph[i] = fmt.Sprintf("$%d", start+i)
		args[i] = id
	}
	return strings.Join(ph, ","), args
}

// MarkReadMany marks the given notifications read; ids that belong to another user are ignored.
func (s *NotificationStore) MarkReadMany(ctx context.Context, userID int64, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	ph, args := inPlaceholders(2, ids)
	_, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET read = TRUE WHERE user_id = $1 AND id IN (`+ph+`)`,
		append([]any{userID}, args...)...,
	)
	return err
}

// NotificationThread identifies the issue, pull request or discussion a notification is about.
type NotificationThread struct {
	RepoID int64
	Kind   string
	Number int64
}

// ThreadsOf returns the distinct threads of userID's notifications among ids. Rows without a
// subject_kind (repo_transfer, unclassifiable mentions) name no thread and are left out.
func (s *NotificationStore) ThreadsOf(ctx context.Context, userID int64, ids []int64) ([]NotificationThread, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ph, args := inPlaceholders(2, ids)
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT repo_id, subject_kind, subject_id FROM notifications
		 WHERE user_id = $1 AND subject_kind IS NOT NULL AND id IN (`+ph+`)
		 ORDER BY repo_id, subject_kind, subject_id`,
		append([]any{userID}, args...)...,
	)
	if err != nil {
		return nil, fmt.Errorf("notification threads: %w", err)
	}
	defer rows.Close()
	var threads []NotificationThread
	for rows.Next() {
		var t NotificationThread
		if err := rows.Scan(&t.RepoID, &t.Kind, &t.Number); err != nil {
			return nil, fmt.Errorf("notification threads: scan: %w", err)
		}
		threads = append(threads, t)
	}
	return threads, rows.Err()
}

// MarkThreadsRead marks read every unread notification of userID on the threads of the notifications
// among ids, selected or not. Ids that aren't userID's or have no subject_kind select no thread.
func (s *NotificationStore) MarkThreadsRead(ctx context.Context, userID int64, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	ph, args := inPlaceholders(2, ids)
	_, err := s.db.ExecContext(ctx,
		`UPDATE notifications n SET read = TRUE
		 WHERE n.user_id = $1 AND n.read = FALSE AND EXISTS (
		   SELECT 1 FROM notifications sel
		   WHERE sel.user_id = $1 AND sel.id IN (`+ph+`) AND sel.subject_kind IS NOT NULL
		     AND sel.repo_id = n.repo_id AND sel.subject_kind = n.subject_kind AND sel.subject_id = n.subject_id)`,
		append([]any{userID}, args...)...,
	)
	if err != nil {
		return fmt.Errorf("notification mark threads read: %w", err)
	}
	return nil
}
