package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mkappworks/cloudzilla/internal/model"
)

type ExploreStore struct{ db *sql.DB }

func NewExploreStore(db *sql.DB) *ExploreStore { return &ExploreStore{db: db} }

// TrendingRepos returns public repos ordered by stars gained since the given cutoff time.
func (s *ExploreStore) TrendingRepos(ctx context.Context, since time.Time, limit int) ([]model.RepositoryWithStats, error) {
	const q = `
SELECT r.id, r.owner_id, r.owner_name, r.org_id, r.name, r.description, r.private,
       r.default_branch, r.created_at, r.updated_at, r.is_fork, r.fork_of_id,
       COUNT(s.repo_id) AS star_count,
       r.fork_count     AS fork_count
FROM repositories r
LEFT JOIN stars s ON s.repo_id = r.id AND s.created_at >= $1
WHERE r.private = FALSE AND r.deleted_at IS NULL
GROUP BY r.id
ORDER BY star_count DESC, r.created_at DESC
LIMIT $2`
	rows, err := s.db.QueryContext(ctx, q, since, limit)
	if err != nil {
		return nil, fmt.Errorf("trending repos: %w", err)
	}
	defer rows.Close()
	return scanReposWithStats(rows)
}

// NewestRepos returns the most recently created public repos.
func (s *ExploreStore) NewestRepos(ctx context.Context, limit int) ([]model.RepositoryWithStats, error) {
	const q = `
SELECT r.id, r.owner_id, r.owner_name, r.org_id, r.name, r.description, r.private,
       r.default_branch, r.created_at, r.updated_at, r.is_fork, r.fork_of_id,
       COALESCE((SELECT COUNT(*) FROM stars s WHERE s.repo_id = r.id), 0) AS star_count,
       r.fork_count AS fork_count
FROM repositories r
WHERE r.private = FALSE AND r.deleted_at IS NULL
ORDER BY r.created_at DESC
LIMIT $1`
	rows, err := s.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("newest repos: %w", err)
	}
	defer rows.Close()
	return scanReposWithStats(rows)
}

// MostForkedRepos returns public repos ordered by fork_count descending.
func (s *ExploreStore) MostForkedRepos(ctx context.Context, limit int) ([]model.RepositoryWithStats, error) {
	const q = `
SELECT r.id, r.owner_id, r.owner_name, r.org_id, r.name, r.description, r.private,
       r.default_branch, r.created_at, r.updated_at, r.is_fork, r.fork_of_id,
       COALESCE((SELECT COUNT(*) FROM stars s WHERE s.repo_id = r.id), 0) AS star_count,
       r.fork_count AS fork_count
FROM repositories r
WHERE r.private = FALSE AND r.deleted_at IS NULL
ORDER BY r.fork_count DESC, r.created_at DESC
LIMIT $1`
	rows, err := s.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("most forked repos: %w", err)
	}
	defer rows.Close()
	return scanReposWithStats(rows)
}

func scanReposWithStats(rows *sql.Rows) ([]model.RepositoryWithStats, error) {
	var repos []model.RepositoryWithStats
	for rows.Next() {
		var r model.RepositoryWithStats
		var orgID, forkOfID sql.NullInt64
		if err := rows.Scan(
			&r.ID, &r.OwnerID, &r.OwnerName, &orgID, &r.Name, &r.Description, &r.Private,
			&r.DefaultBranch, &r.CreatedAt, &r.UpdatedAt, &r.IsFork, &forkOfID,
			&r.StarCount, &r.ForkCount,
		); err != nil {
			return nil, fmt.Errorf("scan repo with stats: %w", err)
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
