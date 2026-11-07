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

// reviewStateExpr is the SQL for a pull request's review state, as the merge check reads it: any changes requested wins,
// then enough approvals (at least one), then "required" when its base branch demands approvals it lacks. The ghost user's
// reviews do not count.
func reviewStateExpr(b *listBuilder, required map[string]int) string {
	branches := make([]string, 0, len(required))
	counts := make([]int32, 0, len(required))
	for br, n := range required {
		branches = append(branches, br)
		counts = append(counts, int32(n))
	}
	req := `COALESCE((SELECT t.n FROM unnest(` + b.arg(branches) + `::text[], ` + b.arg(counts) + `::int[]) AS t(branch, n)
		WHERE t.branch = pr.base_branch), 0)`
	return `(CASE
		WHEN EXISTS (SELECT 1 FROM pull_reviews rv WHERE rv.pull_id = pr.id AND rv.state = 'changes_requested' AND rv.author_id <> ghost_user_id())
			THEN 'changes_requested'
		WHEN (SELECT COUNT(*) FROM pull_reviews rv WHERE rv.pull_id = pr.id AND rv.state = 'approved' AND rv.author_id <> ghost_user_id())
			>= GREATEST(` + req + `, 1) THEN 'approved'
		WHEN ` + req + ` > 0 THEN 'required'
		ELSE 'none' END)`
}

// ReviewStates returns each pull request's review state, keyed by ID.
func (s *PullStore) ReviewStates(ctx context.Context, pullIDs []int64, required map[string]int) (map[int64]string, error) {
	out := make(map[int64]string, len(pullIDs))
	if len(pullIDs) == 0 {
		return out, nil
	}
	b := &listBuilder{}
	expr := reviewStateExpr(b, required)
	rows, err := s.db.QueryContext(ctx,
		`SELECT pr.id, `+expr+` FROM pull_requests pr WHERE pr.id = ANY(`+b.arg(pullIDs)+`::bigint[])`, b.args...)
	if err != nil {
		return nil, fmt.Errorf("pr review states: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var state string
		if err := rows.Scan(&id, &state); err != nil {
			return nil, fmt.Errorf("pr review states scan: %w", err)
		}
		out[id] = state
	}
	return out, rows.Err()
}

// BaseBranches returns the distinct base branches of the repo's pull requests.
func (s *PullStore) BaseBranches(ctx context.Context, repoID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT base_branch FROM pull_requests WHERE repo_id = $1`, repoID)
	if err != nil {
		return nil, fmt.Errorf("pr base branches: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var br string
		if err := rows.Scan(&br); err != nil {
			return nil, fmt.Errorf("pr base branches scan: %w", err)
		}
		out = append(out, br)
	}
	return out, rows.Err()
}
