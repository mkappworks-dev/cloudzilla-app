package store

import (
	"context"
	"fmt"
	"strings"
)

// ListInvolving returns one page of the issues that involve userID (authored, assigned or mentioned) in repos they can
// read, narrowed by f, plus the total across all pages.
func (s *IssueStore) ListInvolving(ctx context.Context, userID int64, f ListFilter) ([]IssueListItem, int, error) {
	b := &listBuilder{args: []any{userID}}
	where := []string{
		"r.deleted_at IS NULL", readableBy("r", "$1"), issueVisibleTo("i", "$1"),
		`(i.author_id = $1
		  OR EXISTS (SELECT 1 FROM issue_assignees ia WHERE ia.issue_id = i.id AND ia.user_id = $1)
		  OR EXISTS (SELECT 1 FROM mentions m JOIN comments c ON c.id = m.comment_id WHERE c.issue_id = i.id AND m.user_id = $1))`,
	}
	if f.State != "" {
		where = append(where, "i.state = "+b.arg(f.State))
	}
	where = append(where, f.where(issueListTable, b)...)
	cond := strings.Join(where, " AND ")
	from := `FROM issues i
	         JOIN repositories r ON r.id = i.repo_id
	         JOIN users u        ON u.id = i.author_id`

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) `+from+` WHERE `+cond, b.args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("issue involving count: %w", err)
	}
	paging := ""
	if f.Limit > 0 {
		paging = " LIMIT " + b.arg(f.Limit) + " OFFSET " + b.arg(f.Offset)
	}
	items, err := s.scanIssueListItems(ctx,
		`SELECT i.id, i.number, i.title, i.state, i.author_id, u.username AS author_name, i.priority, i.visibility,
		        r.owner_name || '/' || r.name AS repo_full_name, i.updated_at, i.created_at
		 `+from+` WHERE `+cond+` ORDER BY `+f.orderBy(issueListTable)+paging, b.args...)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
