package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store/db"
)

type IssueStore struct {
	q  *db.Queries
	db *sql.DB
}

func NewIssueStore(q *db.Queries, database *sql.DB) *IssueStore {
	return &IssueStore{q: q, db: database}
}

func (s *IssueStore) Create(ctx context.Context, issue *model.Issue) error {
	if issue.Visibility == "" {
		issue.Visibility = "public"
	}
	// Get next number for repo
	num, err := s.q.GetNextIssueNumber(ctx, issue.RepoID)
	if err != nil {
		return fmt.Errorf("issue next num: %w", err)
	}
	issue.Number = int(num)

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
		return s.q.UpdateIssueStateClosed(ctx, db.UpdateIssueStateClosedParams{
			State:     string(state),
			ClosedAt:  sql.NullTime{Time: now, Valid: true},
			UpdatedAt: now,
			ID:        id,
		})
	}
	return s.q.UpdateIssueStateOpen(ctx, db.UpdateIssueStateOpenParams{
		State:     string(state),
		UpdatedAt: now,
		ID:        id,
	})
}

func mapDBIssueToModel(dbIssue *db.Issue) *model.Issue {
	issue := &model.Issue{
		ID:        dbIssue.ID,
		RepoID:    dbIssue.RepoID,
		Number:    int(dbIssue.Number),
		AuthorID:  dbIssue.AuthorID,
		Title:     dbIssue.Title,
		Body:      dbIssue.Body,
		State:     model.IssueState(dbIssue.State),
		CreatedAt: dbIssue.CreatedAt,
		UpdatedAt: dbIssue.UpdatedAt,
	}
	if dbIssue.ClosedAt.Valid {
		issue.ClosedAt = &dbIssue.ClosedAt.Time
	}
	return issue
}

func mapDBIssuesToModel(dbIssues []db.Issue) []model.Issue {
	issues := make([]model.Issue, len(dbIssues))
	for i, dbIssue := range dbIssues {
		issues[i] = *mapDBIssueToModel(&dbIssue)
	}
	return issues
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
