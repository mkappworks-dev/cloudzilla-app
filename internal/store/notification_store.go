package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

const notificationCols = `id, user_id, actor_id, actor_name, type, repo_id, repo_name, owner_name, subject_id, subject_url, subject_title, COALESCE(subject_kind, ''), read, created_at`

// Inbox filters for ListThreadPage and CountThreads.
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

// Inbox reasons for ListThreadPage and CountThreads; empty means any.
const (
	NotifReasonMention         = "mention"
	NotifReasonReviewRequested = "review_requested"
	NotifReasonAssigned        = "assigned"
	NotifReasonSubscribed      = "subscribed"
)

// reasonClause returns a fixed SQL fragment on alias n, never one built from input; an unknown
// reason means any. Mention is a property of the notification; the others are the user's standing
// on the thread now, not when the notification was sent.
func reasonClause(reason string) string {
	switch reason {
	case NotifReasonMention:
		return ` AND n.type = 'mention'`
	case NotifReasonReviewRequested:
		return ` AND n.subject_kind = 'pull' AND EXISTS (SELECT 1 FROM pull_requests p
		   JOIN pull_reviews rv ON rv.pull_id = p.id AND rv.author_id = n.user_id AND rv.state = 'pending'
		   WHERE p.repo_id = n.repo_id AND p.number = n.subject_id)`
	case NotifReasonAssigned:
		return ` AND (n.subject_kind = 'issue' AND EXISTS (SELECT 1 FROM issues i
		   JOIN issue_assignees a ON a.issue_id = i.id AND a.user_id = n.user_id
		   WHERE i.repo_id = n.repo_id AND i.number = n.subject_id)
		 OR n.subject_kind = 'pull' AND EXISTS (SELECT 1 FROM pull_requests p
		   JOIN pull_assignees a ON a.pull_id = p.id AND a.user_id = n.user_id
		   WHERE p.repo_id = n.repo_id AND p.number = n.subject_id))`
	case NotifReasonSubscribed:
		return ` AND EXISTS (SELECT 1 FROM thread_subscriptions ts
		   WHERE ts.user_id = n.user_id AND ts.repo_id = n.repo_id AND ts.kind = n.subject_kind
		     AND ts.number = n.subject_id AND ts.state = 'subscribed')`
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

// MarkDone marks read the given notifications and, for each that names a thread, the thread's older
// ones: the group its inbox row stood for. A newer notification, which arrived after the row was
// shown, stays unread. Ids that belong to another user are ignored.
func (s *NotificationStore) MarkDone(ctx context.Context, userID int64, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	ph, args := inPlaceholders(2, ids)
	_, err := s.db.ExecContext(ctx,
		`UPDATE notifications n SET read = TRUE
		 WHERE n.user_id = $1 AND n.read = FALSE AND EXISTS (
		   SELECT 1 FROM notifications sel
		   WHERE sel.user_id = $1 AND sel.id IN (`+ph+`) AND (sel.id = n.id
		     OR sel.subject_kind IS NOT NULL AND (n.created_at, n.id) < (sel.created_at, sel.id)
		       AND sel.repo_id = n.repo_id AND sel.subject_kind = n.subject_kind AND sel.subject_id = n.subject_id))`,
		append([]any{userID}, args...)...,
	)
	if err != nil {
		return fmt.Errorf("notification mark done: %w", err)
	}
	return nil
}

// MarkUnread marks userID's notifications among ids unread. Ids that belong to another user are ignored.
func (s *NotificationStore) MarkUnread(ctx context.Context, userID int64, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	ph, args := inPlaceholders(2, ids)
	_, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET read = FALSE WHERE user_id = $1 AND id IN (`+ph+`)`,
		append([]any{userID}, args...)...,
	)
	if err != nil {
		return fmt.Errorf("notification mark unread: %w", err)
	}
	return nil
}

// ThreadHeadIDs returns the id of the newest notification in each group ListThreadPage pages through,
// newest group first: what "select all" stands for when the groups outnumber a page.
func (s *NotificationStore) ThreadHeadIDs(ctx context.Context, userID int64, filter, reason string) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM (
		   SELECT DISTINCT ON (repo_id, subject_kind, subject_id, solo) id, created_at
		   FROM (`+threadRows(filter, reason)+`) t
		   ORDER BY repo_id, subject_kind, subject_id, solo, created_at DESC, id DESC
		 ) g
		 ORDER BY created_at DESC, id DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("notification thread heads: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("notification thread heads: scan: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
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

// threadRows selects userID's notifications matching filter and reason, with solo set to the id of
// a row that names no thread so it never merges with another.
func threadRows(filter, reason string) string {
	return `SELECT n.*, CASE WHEN n.subject_kind IS NULL THEN n.id END AS solo
		FROM notifications n WHERE n.user_id = $1` + filterClause(filter) + reasonClause(reason)
}

// ListThreadPage returns one page of userID's notification groups, newest first. Filter and reason
// pick the notifications before grouping, so a group counts only its matching ones.
func (s *NotificationStore) ListThreadPage(ctx context.Context, userID int64, filter, reason string, limit, offset int) ([]model.NotificationGroup, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+notificationCols+`, count, unread FROM (
		   SELECT DISTINCT ON (repo_id, subject_kind, subject_id, solo) *,
		     COUNT(*) OVER w AS count, COUNT(*) FILTER (WHERE NOT read) OVER w AS unread
		   FROM (`+threadRows(filter, reason)+`) t
		   WINDOW w AS (PARTITION BY repo_id, subject_kind, subject_id, solo)
		   ORDER BY repo_id, subject_kind, subject_id, solo, created_at DESC, id DESC
		 ) g
		 ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`,
		userID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("notification list thread page: %w", err)
	}
	defer rows.Close()
	var groups []model.NotificationGroup
	for rows.Next() {
		var g model.NotificationGroup
		if err := rows.Scan(&g.ID, &g.UserID, &g.ActorID, &g.ActorName, &g.Type, &g.RepoID, &g.RepoName, &g.OwnerName, &g.SubjectID, &g.SubjectURL, &g.SubjectTitle, &g.SubjectKind, &g.Read, &g.CreatedAt, &g.Count, &g.Unread); err != nil {
			return nil, fmt.Errorf("notification list thread page: scan: %w", err)
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

// CountThreads counts the groups ListThreadPage pages through.
func (s *NotificationStore) CountThreads(ctx context.Context, userID int64, filter, reason string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM (SELECT DISTINCT repo_id, subject_kind, subject_id, solo FROM (`+threadRows(filter, reason)+`) t) g`,
		userID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("notification count threads: %w", err)
	}
	return count, nil
}

// renameNotificationText relabels the notifications of repoID and moves the
// /owner/oldName prefix of their subject_url to newName.
func renameNotificationText(ctx context.Context, x dbExec, repoID int64, ownerName, oldName, newName string) error {
	if _, err := x.ExecContext(ctx,
		`UPDATE notifications SET owner_name = $2, repo_name = $3,
		        subject_url = CASE WHEN left(subject_url, length($4::text) + 1) = $4::text || '/'
		                           THEN '/' || $2::text || '/' || $3::text || substr(subject_url, length($4::text) + 1)
		                           ELSE subject_url END
		 WHERE repo_id = $1`,
		repoID, ownerName, newName, "/"+ownerName+"/"+oldName,
	); err != nil {
		return fmt.Errorf("rename notification text: %w", err)
	}
	return nil
}
