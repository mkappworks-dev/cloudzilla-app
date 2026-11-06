package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// ListFiltered returns one page of the repo's pull requests that f matches, plus the total across all pages.
func (s *PullStore) ListFiltered(ctx context.Context, repoID int64, f ListFilter) ([]model.PullRequest, int, error) {
	b := &listBuilder{args: []any{repoID}}
	where := []string{"pr.repo_id = $1"}
	switch f.State {
	case "open":
		where = append(where, "pr.state = 'open' AND NOT pr.is_draft")
	case "draft":
		where = append(where, "pr.state = 'open' AND pr.is_draft")
	case "merged", "closed":
		where = append(where, "pr.state = "+b.arg(f.State))
	}
	where = append(where, f.where(pullListTable, b)...)
	cond := strings.Join(where, " AND ")

	var total int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pull_requests pr LEFT JOIN users u ON u.id = pr.author_id WHERE `+cond, b.args...,
	).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("pr list count: %w", err)
	}

	paging := ""
	if f.Limit > 0 {
		paging = " LIMIT " + b.arg(f.Limit) + " OFFSET " + b.arg(f.Offset)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT pr.id, pr.repo_id, pr.number, pr.author_id, COALESCE(u.username, '') AS author_name,
		        pr.title, pr.body, pr.state, pr.head_branch, pr.base_branch,
		        pr.created_at, pr.updated_at, pr.merged_at, pr.closed_at, pr.is_draft, pr.draft_at,
		        pr.auto_merge_enabled, pr.auto_merge_strategy, pr.head_sha
		 FROM pull_requests pr
		 LEFT JOIN users u ON u.id = pr.author_id
		 WHERE `+cond+`
		 ORDER BY `+f.orderBy(pullListTable)+paging, b.args...)
	if err != nil {
		return nil, 0, fmt.Errorf("pr list filtered: %w", err)
	}
	defer rows.Close()
	prs, err := scanPullRows(rows)
	if err != nil {
		return nil, 0, err
	}
	return prs, total, nil
}

// PullStateCounts are a repo's pull request totals per list tab.
type PullStateCounts struct {
	Open, Draft, Merged, Closed int
}

// CountByState returns the per-tab pull request totals, ignoring any filter.
func (s *PullStore) CountByState(ctx context.Context, repoID int64) (PullStateCounts, error) {
	var c PullStateCounts
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FILTER (WHERE state = 'open' AND NOT is_draft),
		        COUNT(*) FILTER (WHERE state = 'open' AND is_draft),
		        COUNT(*) FILTER (WHERE state = 'merged'),
		        COUNT(*) FILTER (WHERE state = 'closed')
		 FROM pull_requests WHERE repo_id = $1`, repoID,
	).Scan(&c.Open, &c.Draft, &c.Merged, &c.Closed)
	if err != nil {
		return c, fmt.Errorf("pr count by state: %w", err)
	}
	return c, nil
}
