package store

import (
	"context"
	"fmt"
	"strings"
)

// ListInvolving returns one page of the pull requests that involve userID (authored, assigned, mentioned or awaiting
// their review) in repos they can read, narrowed by f, plus the total across all pages.
func (s *PullStore) ListInvolving(ctx context.Context, userID int64, f ListFilter) ([]PullListItem, int, error) {
	b := &listBuilder{args: []any{userID}}
	where := []string{
		"r.deleted_at IS NULL", readableBy("r", "$1"),
		`(pr.author_id = $1
		  OR EXISTS (SELECT 1 FROM pull_assignees pa WHERE pa.pull_id = pr.id AND pa.user_id = $1)
		  OR EXISTS (SELECT 1 FROM pull_reviews prv WHERE prv.pull_id = pr.id AND prv.author_id = $1 AND prv.state = 'pending')
		  OR EXISTS (SELECT 1 FROM mentions m JOIN comments c ON c.id = m.comment_id WHERE c.pull_id = pr.id AND m.user_id = $1))`,
	}
	// Unlike a repo's Open tab, the account list keeps drafts under Open and shows them with a Draft badge.
	if f.State == "open" {
		where = append(where, "pr.state = 'open'")
	} else {
		where = append(where, pullStateCond(b, f.State))
	}
	where = append(where, f.where(pullListTable, b)...)
	cond := strings.Join(where, " AND ")
	from := `FROM pull_requests pr
	         JOIN repositories r ON r.id = pr.repo_id
	         LEFT JOIN users u   ON u.id = pr.author_id`

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) `+from+` WHERE `+cond, b.args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("pr involving count: %w", err)
	}
	paging := ""
	if f.Limit > 0 {
		paging = " LIMIT " + b.arg(f.Limit) + " OFFSET " + b.arg(f.Offset)
	}
	items, err := s.scanPullListItems(ctx,
		`SELECT pr.id, pr.number, pr.title, pr.state, pr.is_draft, pr.author_id,
		        r.owner_name || '/' || r.name AS repo_full_name, pr.updated_at, pr.created_at
		 `+from+` WHERE `+cond+` ORDER BY `+f.orderBy(pullListTable)+paging, b.args...)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
