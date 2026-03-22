package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
)

type SearchStore struct{ db *sql.DB }

func NewSearchStore(db *sql.DB) *SearchStore { return &SearchStore{db: db} }

func (s *SearchStore) SearchRepos(ctx context.Context, query string, requestingUserID *int64, limit int) ([]model.Repository, error) {
	const q = `
SELECT r.id, r.owner_id, r.owner_name, r.org_id, r.name, r.description, r.private,
       r.default_branch, r.created_at, r.updated_at, r.is_fork, r.fork_of_id, r.fork_count
FROM repositories r
WHERE r.search_vector @@ plainto_tsquery('english', $1)
  AND (r.private = FALSE OR r.owner_id = $2)
ORDER BY ts_rank(r.search_vector, plainto_tsquery('english', $1)) DESC
LIMIT $3`

	var ownerID int64
	if requestingUserID != nil {
		ownerID = *requestingUserID
	}
	rows, err := s.db.QueryContext(ctx, q, query, ownerID, limit)
	if err != nil {
		return nil, fmt.Errorf("search repos: %w", err)
	}
	defer rows.Close()
	return scanSearchRepos(rows)
}

func (s *SearchStore) SearchIssues(ctx context.Context, query string, limit int) ([]model.Issue, error) {
	const q = `
SELECT i.id, i.repo_id, i.number, i.author_id, u.username, i.title, i.body, i.state,
       i.created_at, i.updated_at, i.closed_at
FROM issues i
JOIN users u ON u.id = i.author_id
JOIN repositories r ON r.id = i.repo_id
WHERE i.search_vector @@ plainto_tsquery('english', $1)
  AND r.private = FALSE
ORDER BY ts_rank(i.search_vector, plainto_tsquery('english', $1)) DESC
LIMIT $2`
	rows, err := s.db.QueryContext(ctx, q, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search issues: %w", err)
	}
	defer rows.Close()
	return scanIssues(rows)
}

func (s *SearchStore) SearchPulls(ctx context.Context, query string, limit int) ([]model.PullRequest, error) {
	const q = `
SELECT p.id, p.repo_id, p.number, p.author_id, u.username, p.title, p.body, p.state,
       p.head_branch, p.base_branch, p.created_at, p.updated_at, p.merged_at, p.closed_at
FROM pull_requests p
JOIN users u ON u.id = p.author_id
JOIN repositories r ON r.id = p.repo_id
WHERE p.search_vector @@ plainto_tsquery('english', $1)
  AND r.private = FALSE
ORDER BY ts_rank(p.search_vector, plainto_tsquery('english', $1)) DESC
LIMIT $2`
	rows, err := s.db.QueryContext(ctx, q, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search pulls: %w", err)
	}
	defer rows.Close()
	return scanPulls(rows)
}

func (s *SearchStore) SearchUsers(ctx context.Context, query string, limit int) ([]model.User, error) {
	const q = `
SELECT id, username, email, bio, avatar_url, created_at, updated_at
FROM users
WHERE lower(username) LIKE lower($1) || '%'
ORDER BY username
LIMIT $2`
	rows, err := s.db.QueryContext(ctx, q, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search users: %w", err)
	}
	defer rows.Close()
	var users []model.User
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.Bio, &u.AvatarURL, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func scanSearchRepos(rows *sql.Rows) ([]model.Repository, error) {
	var repos []model.Repository
	for rows.Next() {
		var r model.Repository
		var orgID, forkOfID sql.NullInt64
		if err := rows.Scan(
			&r.ID, &r.OwnerID, &r.OwnerName, &orgID, &r.Name, &r.Description, &r.Private,
			&r.DefaultBranch, &r.CreatedAt, &r.UpdatedAt, &r.IsFork, &forkOfID, &r.ForkCount,
		); err != nil {
			return nil, err
		}
		if orgID.Valid {
			r.OrgID = orgID.Int64
		}
		if forkOfID.Valid {
			r.ForkOfID = &forkOfID.Int64
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

func scanIssues(rows *sql.Rows) ([]model.Issue, error) {
	var issues []model.Issue
	for rows.Next() {
		var i model.Issue
		var closedAt sql.NullTime
		if err := rows.Scan(
			&i.ID, &i.RepoID, &i.Number, &i.AuthorID, &i.AuthorName,
			&i.Title, &i.Body, &i.State, &i.CreatedAt, &i.UpdatedAt, &closedAt,
		); err != nil {
			return nil, err
		}
		if closedAt.Valid {
			i.ClosedAt = &closedAt.Time
		}
		issues = append(issues, i)
	}
	return issues, rows.Err()
}

func scanPulls(rows *sql.Rows) ([]model.PullRequest, error) {
	var pulls []model.PullRequest
	for rows.Next() {
		var p model.PullRequest
		var mergedAt, closedAt sql.NullTime
		if err := rows.Scan(
			&p.ID, &p.RepoID, &p.Number, &p.AuthorID, &p.AuthorName,
			&p.Title, &p.Body, &p.State, &p.HeadBranch, &p.BaseBranch,
			&p.CreatedAt, &p.UpdatedAt, &mergedAt, &closedAt,
		); err != nil {
			return nil, err
		}
		if mergedAt.Valid {
			p.MergedAt = &mergedAt.Time
		}
		if closedAt.Valid {
			p.ClosedAt = &closedAt.Time
		}
		pulls = append(pulls, p)
	}
	return pulls, rows.Err()
}
