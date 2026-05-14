package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// IssueStore provides database operations for issues.
type IssueStore struct {
	db *sql.DB
}

// NewIssueStore creates an IssueStore backed by the given database.
func NewIssueStore(database *sql.DB) *IssueStore {
	return &IssueStore{db: database}
}

func (s *IssueStore) Create(ctx context.Context, issue *model.Issue) error {
	if issue.Visibility == "" {
		issue.Visibility = "public"
	}
	// Get next number for repo
	var num int
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(number), 0) + 1 FROM issues WHERE repo_id = $1`,
		issue.RepoID,
	).Scan(&num)
	if err != nil {
		return fmt.Errorf("issue next num: %w", err)
	}
	issue.Number = num

	now := time.Now().UTC()
	err = s.db.QueryRowContext(ctx,
		`INSERT INTO issues (repo_id, number, author_id, title, body, state, visibility, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		issue.RepoID, issue.Number, issue.AuthorID, issue.Title, issue.Body,
		string(issue.State), issue.Visibility, now, now,
	).Scan(&issue.ID)
	if err != nil {
		return fmt.Errorf("issue create: %w", err)
	}
	issue.CreatedAt = now
	issue.UpdatedAt = now
	return nil
}

func (s *IssueStore) List(ctx context.Context, repoID int64) ([]model.Issue, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT i.id, i.repo_id, i.number, i.author_id,
		        COALESCE(u.username, '') AS author_name,
		        i.title, i.body, i.state,
		        i.milestone_id, i.visibility, i.created_at, i.updated_at, i.closed_at,
		        i.is_pinned, i.is_locked, i.locked_at
		 FROM issues i
		 LEFT JOIN users u ON u.id = i.author_id
		 WHERE i.repo_id = $1
		 ORDER BY i.created_at DESC`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("issue list: %w", err)
	}
	defer rows.Close()
	issues, err := scanIssueRows(rows)
	if err != nil {
		return nil, fmt.Errorf("issue list: %w", err)
	}
	return issues, nil
}

// GetByNumber returns a single issue by repo + number.
// If visibleToUserID is nil, only public issues are returned.
// If visibleToUserID is set, the issue is also returned when the user is the
// author or has at least writer/admin/owner permission on the repo.
func (s *IssueStore) GetByNumber(ctx context.Context, repoID int64, number int, visibleToUserID *int64) (*model.Issue, error) {
	visClause := `AND (i.visibility = 'public'`
	args := []interface{}{repoID, number}
	argIdx := 3

	if visibleToUserID != nil {
		args = append(args, *visibleToUserID, *visibleToUserID)
		visClause += fmt.Sprintf(
			` OR i.author_id = $%d OR EXISTS (
				SELECT 1 FROM permissions p
				WHERE p.user_id = $%d AND p.repo_id = i.repo_id
				  AND p.role IN ('owner','admin','writer')
			)`, argIdx, argIdx+1)
		argIdx += 2
	}
	visClause += `)`
	_ = argIdx

	q := fmt.Sprintf(`
		SELECT i.id, i.repo_id, i.number, i.author_id,
		       COALESCE(u.username, '') AS author_name,
		       i.title, i.body, i.state,
		       i.milestone_id, i.visibility,
		       i.created_at, i.updated_at, i.closed_at,
		       i.is_pinned, i.is_locked, i.locked_at
		FROM issues i
		LEFT JOIN users u ON u.id = i.author_id
		WHERE i.repo_id = $1 AND i.number = $2
		%s
		LIMIT 1`, visClause)

	var iss model.Issue
	var closedAt, lockedAt sql.NullTime
	var milestoneID sql.NullInt64
	err := s.db.QueryRowContext(ctx, q, args...).Scan(
		&iss.ID, &iss.RepoID, &iss.Number, &iss.AuthorID, &iss.AuthorName,
		&iss.Title, &iss.Body, &iss.State,
		&milestoneID, &iss.Visibility,
		&iss.CreatedAt, &iss.UpdatedAt, &closedAt,
		&iss.IsPinned, &iss.IsLocked, &lockedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("issue get: %w", err)
	}
	if closedAt.Valid {
		iss.ClosedAt = &closedAt.Time
	}
	if lockedAt.Valid {
		iss.LockedAt = &lockedAt.Time
	}
	if milestoneID.Valid {
		iss.MilestoneID = &milestoneID.Int64
	}
	return &iss, nil
}

// GetByNumberUnfiltered returns an issue regardless of visibility.
// Use only in internal service operations that have already enforced their own authorization.
func (s *IssueStore) GetByNumberUnfiltered(ctx context.Context, repoID int64, number int) (*model.Issue, error) {
	var iss model.Issue
	var closedAt, lockedAt sql.NullTime
	var milestoneID sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT i.id, i.repo_id, i.number, i.author_id,
		       COALESCE(u.username, '') AS author_name,
		       i.title, i.body, i.state,
		       i.milestone_id, i.visibility,
		       i.created_at, i.updated_at, i.closed_at,
		       i.is_pinned, i.is_locked, i.locked_at
		FROM issues i
		LEFT JOIN users u ON u.id = i.author_id
		WHERE i.repo_id = $1 AND i.number = $2
		LIMIT 1`,
		repoID, number,
	).Scan(
		&iss.ID, &iss.RepoID, &iss.Number, &iss.AuthorID, &iss.AuthorName,
		&iss.Title, &iss.Body, &iss.State,
		&milestoneID, &iss.Visibility,
		&iss.CreatedAt, &iss.UpdatedAt, &closedAt,
		&iss.IsPinned, &iss.IsLocked, &lockedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("issue get: %w", err)
	}
	if closedAt.Valid {
		iss.ClosedAt = &closedAt.Time
	}
	if lockedAt.Valid {
		iss.LockedAt = &lockedAt.Time
	}
	if milestoneID.Valid {
		iss.MilestoneID = &milestoneID.Int64
	}
	return &iss, nil
}

// ListByRepo returns issues for the given repo, filtered by optional state and visibility.
// If visibleToUserID is nil, only public issues are returned.
// If visibleToUserID is set, public issues plus private issues authored by that user or
// for which that user has at least writer/admin/owner permission are returned.
func (s *IssueStore) ListByRepo(ctx context.Context, repoID int64, state *string, visibleToUserID *int64, page, pageSize int) ([]model.Issue, error) {
	offset := (page - 1) * pageSize

	visClause := `AND (i.visibility = 'public'`
	args := []interface{}{repoID}
	argIdx := 2

	if visibleToUserID != nil {
		args = append(args, *visibleToUserID, *visibleToUserID)
		visClause += fmt.Sprintf(
			` OR i.author_id = $%d OR EXISTS (
				SELECT 1 FROM permissions p
				WHERE p.user_id = $%d AND p.repo_id = i.repo_id
				  AND p.role IN ('owner','admin','writer')
			)`, argIdx, argIdx+1)
		argIdx += 2
	}
	visClause += `)`

	stateClause := ""
	if state != nil {
		stateClause = fmt.Sprintf(" AND i.state = $%d", argIdx)
		args = append(args, *state)
		argIdx++
	}

	args = append(args, pageSize, offset)
	limitClause := fmt.Sprintf(" LIMIT $%d OFFSET $%d", argIdx, argIdx+1)

	q := fmt.Sprintf(`
		SELECT i.id, i.repo_id, i.number, i.author_id,
		       COALESCE(u.username, '') AS author_name,
		       i.title, i.body, i.state,
		       i.milestone_id, i.visibility,
		       i.created_at, i.updated_at, i.closed_at,
		       i.is_pinned, i.is_locked, i.locked_at
		FROM issues i
		LEFT JOIN users u ON u.id = i.author_id
		WHERE i.repo_id = $1
		%s
		%s
		ORDER BY i.created_at DESC
		%s`, visClause, stateClause, limitClause)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("issue list by repo: %w", err)
	}
	defer rows.Close()
	return scanIssueRows(rows)
}

func (s *IssueStore) UpdateState(ctx context.Context, id int64, state model.IssueState) error {
	now := time.Now().UTC()
	if state == model.IssueStateClosed {
		_, err := s.db.ExecContext(ctx,
			`UPDATE issues SET state = $1, closed_at = $2, updated_at = $3 WHERE id = $4`,
			string(state), sql.NullTime{Time: now, Valid: true}, now, id,
		)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE issues SET state = $1, closed_at = NULL, updated_at = $2 WHERE id = $3`,
		string(state), now, id,
	)
	return err
}

// CountPinnedByRepo returns the number of currently pinned issues in a repo.
func (s *IssueStore) CountPinnedByRepo(ctx context.Context, repoID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM issues WHERE repo_id = $1 AND is_pinned = TRUE`,
		repoID,
	).Scan(&count)
	return count, err
}

// SetPinned pins or unpins an issue.
func (s *IssueStore) SetPinned(ctx context.Context, issueID int64, pinned bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE issues SET is_pinned = $1, updated_at = NOW() WHERE id = $2`,
		pinned, issueID,
	)
	return err
}

// SetLocked locks or unlocks an issue. When locking, locked_at is set to NOW(); when unlocking it is cleared.
func (s *IssueStore) SetLocked(ctx context.Context, issueID int64, locked bool) error {
	if locked {
		_, err := s.db.ExecContext(ctx,
			`UPDATE issues SET is_locked = TRUE, locked_at = NOW(), updated_at = NOW() WHERE id = $1`,
			issueID,
		)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE issues SET is_locked = FALSE, locked_at = NULL, updated_at = NOW() WHERE id = $1`,
		issueID,
	)
	return err
}

// ListPinned returns all pinned issues for a repo, ordered by number ascending.
func (s *IssueStore) ListPinned(ctx context.Context, repoID int64) ([]model.Issue, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT i.id, i.repo_id, i.number, i.author_id,
		        COALESCE(u.username, '') AS author_name,
		        i.title, i.body, i.state,
		        i.milestone_id, i.visibility, i.created_at, i.updated_at, i.closed_at,
		        i.is_pinned, i.is_locked, i.locked_at
		 FROM issues i
		 LEFT JOIN users u ON u.id = i.author_id
		 WHERE i.repo_id = $1 AND i.is_pinned = TRUE
		 ORDER BY i.number ASC`,
		repoID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanIssueRows(rows)
}

func scanIssueRows(rows *sql.Rows) ([]model.Issue, error) {
	var issues []model.Issue
	for rows.Next() {
		var iss model.Issue
		var closedAt, lockedAt sql.NullTime
		var milestoneID sql.NullInt64
		if err := rows.Scan(
			&iss.ID, &iss.RepoID, &iss.Number, &iss.AuthorID,
			&iss.AuthorName, &iss.Title, &iss.Body, &iss.State,
			&milestoneID, &iss.Visibility, &iss.CreatedAt, &iss.UpdatedAt, &closedAt,
			&iss.IsPinned, &iss.IsLocked, &lockedAt,
		); err != nil {
			return nil, err
		}
		if closedAt.Valid {
			iss.ClosedAt = &closedAt.Time
		}
		if lockedAt.Valid {
			iss.LockedAt = &lockedAt.Time
		}
		if milestoneID.Valid {
			iss.MilestoneID = &milestoneID.Int64
		}
		issues = append(issues, iss)
	}
	return issues, rows.Err()
}

func (s *IssueStore) CountCreatedSince(ctx context.Context, repoID int64, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM issues WHERE repo_id = $1 AND created_at >= $2`,
		repoID, since,
	).Scan(&n)
	return n, err
}

func (s *IssueStore) CountClosedSince(ctx context.Context, repoID int64, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM issues WHERE repo_id = $1 AND closed_at >= $2`,
		repoID, since,
	).Scan(&n)
	return n, err
}

// CountOpenAuthoredByOrAssignedTo returns the count of open issues the
// user either authored OR is assigned to.
func (s *IssueStore) CountOpenAuthoredByOrAssignedTo(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT i.id)
		 FROM issues i
		 LEFT JOIN issue_assignees a ON a.issue_id = i.id
		 WHERE i.state = 'open' AND (i.author_id = $1 OR a.user_id = $1)`,
		userID,
	).Scan(&n)
	return n, err
}

// IssueListItem is a display-ready issue projection for cross-repo lists
// like the home page attention list. It includes the repo's full name so
// callers do not have to make extra round trips for repo metadata.
type IssueListItem struct {
	ID           int64
	Number       int
	Title        string
	AuthorID     int64
	RepoFullName string // "<owner_username>/<repo_name>"
	UpdatedAt    time.Time
}

// ListOpenAssignedToUser returns up to 50 open issues assigned to the
// given user, joined with the repo's full name for display. Sorted by
// updated_at descending. Used by the home page attention list.
func (s *IssueStore) ListOpenAssignedToUser(ctx context.Context, userID int64) ([]IssueListItem, error) {
	const q = `
		SELECT i.id, i.number, i.title, i.author_id,
		       u.username || '/' || r.name AS repo_full_name,
		       i.updated_at
		FROM issues i
		JOIN issue_assignees a ON a.issue_id = i.id
		JOIN repositories r    ON r.id = i.repo_id
		JOIN users u           ON u.id = r.owner_id
		WHERE a.user_id = $1 AND i.state = 'open'
		ORDER BY i.updated_at DESC
		LIMIT 50
	`
	rows, err := s.db.QueryContext(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IssueListItem
	for rows.Next() {
		var it IssueListItem
		if err := rows.Scan(&it.ID, &it.Number, &it.Title, &it.AuthorID, &it.RepoFullName, &it.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}
