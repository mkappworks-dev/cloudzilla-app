package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// ListFiltered returns one page of the repo's issues that the viewer can see and f matches, plus the total across all
// pages.
func (s *IssueStore) ListFiltered(ctx context.Context, repoID int64, visibleToUserID *int64, f ListFilter) ([]model.Issue, int, error) {
	b := &listBuilder{args: []any{repoID, viewerID(visibleToUserID)}}
	where := []string{"i.repo_id = $1", issueVisibleTo("i", "$2")}
	if f.State != "" {
		where = append(where, "i.state = "+b.arg(f.State))
	}
	where = append(where, f.where(issueListTable, b)...)
	cond := strings.Join(where, " AND ")

	var total int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM issues i LEFT JOIN users u ON u.id = i.author_id WHERE `+cond, b.args...,
	).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("issue list count: %w", err)
	}

	paging := ""
	if f.Limit > 0 {
		paging = " LIMIT " + b.arg(f.Limit) + " OFFSET " + b.arg(f.Offset)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.id, i.repo_id, i.number, i.author_id,
		       COALESCE(u.username, '') AS author_name,
		       i.title, i.body, i.state, i.priority,
		       i.milestone_id, i.visibility,
		       i.created_at, i.updated_at, i.closed_at,
		       i.is_pinned, i.is_locked, i.locked_at
		FROM issues i
		LEFT JOIN users u ON u.id = i.author_id
		WHERE `+cond+`
		ORDER BY `+f.orderBy(issueListTable)+paging, b.args...)
	if err != nil {
		return nil, 0, fmt.Errorf("issue list filtered: %w", err)
	}
	defer rows.Close()
	issues, err := scanIssueRows(rows)
	if err != nil {
		return nil, 0, err
	}
	return issues, total, nil
}

// CountByState returns the viewer-visible open and closed issue totals, ignoring any filter.
func (s *IssueStore) CountByState(ctx context.Context, repoID int64, visibleToUserID *int64) (open, closed int, err error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT i.state, COUNT(*) FROM issues i WHERE i.repo_id = $1 AND `+issueVisibleTo("i", "$2")+` GROUP BY i.state`,
		repoID, viewerID(visibleToUserID))
	if err != nil {
		return 0, 0, fmt.Errorf("issue count by state: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			return 0, 0, fmt.Errorf("issue count by state scan: %w", err)
		}
		if state == "open" {
			open = n
		} else {
			closed += n
		}
	}
	return open, closed, rows.Err()
}
