package store

import (
	"context"
	"fmt"
)

// ListParticipants returns the usernames that authored or were assigned an issue the viewer can see, or a pull request,
// in the repo: the choices of the list's Author and Assignee menus. It stops at 200 names; past that a user is reachable
// by typing author:name.
func (s *IssueStore) ListParticipants(ctx context.Context, repoID int64, visibleToUserID *int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.username FROM users u WHERE u.id IN (
			SELECT i.author_id FROM issues i WHERE i.repo_id = $1 AND `+issueVisibleTo("i", "$2")+`
			UNION SELECT ia.user_id FROM issue_assignees ia JOIN issues i ON i.id = ia.issue_id
				WHERE i.repo_id = $1 AND `+issueVisibleTo("i", "$2")+`
			UNION SELECT pr.author_id FROM pull_requests pr WHERE pr.repo_id = $1
			UNION SELECT pa.user_id FROM pull_assignees pa JOIN pull_requests pr ON pr.id = pa.pull_id WHERE pr.repo_id = $1
		) ORDER BY LOWER(u.username) LIMIT 200`, repoID, viewerID(visibleToUserID))
	if err != nil {
		return nil, fmt.Errorf("list participants: %w", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("list participants scan: %w", err)
		}
		names = append(names, n)
	}
	return names, rows.Err()
}
