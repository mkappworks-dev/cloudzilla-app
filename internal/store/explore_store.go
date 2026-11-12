package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// ExploreStore provides database queries for trending repositories and users.
type ExploreStore struct{ db *sql.DB }

// NewExploreStore creates an ExploreStore backed by the given database.
func NewExploreStore(db *sql.DB) *ExploreStore { return &ExploreStore{db: db} }

// ExploreFilter narrows and pages the public repository listing.
type ExploreFilter struct {
	Text     string
	Language string
	Limit    int
	Offset   int
}

// ExploreOrder picks how ListPublicRepos sorts.
type ExploreOrder int

const (
	ExploreByTrending ExploreOrder = iota
	ExploreByNewest
	ExploreByForks
)

func exploreWhere(f ExploreFilter, args []any) (string, []any) {
	w := `r.private = FALSE AND r.deleted_at IS NULL`
	if f.Text != "" {
		args = append(args, "%"+escapeLike(f.Text)+"%")
		n := strconv.Itoa(len(args))
		w += ` AND (r.name ILIKE $` + n + ` ESCAPE '\' OR r.owner_name ILIKE $` + n + ` ESCAPE '\' OR r.description ILIKE $` + n + ` ESCAPE '\')`
	}
	if f.Language != "" {
		args = append(args, f.Language)
		w += ` AND r.primary_language = $` + strconv.Itoa(len(args))
	}
	return w, args
}

// ListPublicRepos returns one page of public repos matching f and the total that match. since only
// affects ExploreByTrending, which ranks by stars gained after it.
func (s *ExploreStore) ListPublicRepos(ctx context.Context, order ExploreOrder, since time.Time, f ExploreFilter) ([]model.RepositoryWithStats, int, error) {
	where, args := exploreWhere(f, nil)

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM repositories r WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count public repos: %w", err)
	}

	const cols = `SELECT r.id, r.owner_id, r.owner_name, r.org_id, r.name, r.description, r.private,
       r.default_branch, r.created_at, r.updated_at, r.is_fork, r.fork_of_id,`
	var q string
	switch order {
	case ExploreByNewest:
		q = cols + `
       COALESCE((SELECT COUNT(*) FROM stars s WHERE s.repo_id = r.id), 0) AS star_count,
       r.fork_count AS fork_count
FROM repositories r WHERE ` + where + `
ORDER BY r.created_at DESC, r.id DESC`
	case ExploreByForks:
		q = cols + `
       COALESCE((SELECT COUNT(*) FROM stars s WHERE s.repo_id = r.id), 0) AS star_count,
       r.fork_count AS fork_count
FROM repositories r WHERE ` + where + `
ORDER BY r.fork_count DESC, r.created_at DESC, r.id DESC`
	default:
		args = append(args, since)
		q = cols + `
       COUNT(s.repo_id) AS star_count,
       r.fork_count     AS fork_count
FROM repositories r
LEFT JOIN stars s ON s.repo_id = r.id AND s.created_at >= $` + strconv.Itoa(len(args)) + `
WHERE ` + where + `
GROUP BY r.id
ORDER BY star_count DESC, r.created_at DESC, r.id DESC`
	}
	args = append(args, f.Limit, f.Offset)
	q += ` LIMIT $` + strconv.Itoa(len(args)-1) + ` OFFSET $` + strconv.Itoa(len(args))

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list public repos: %w", err)
	}
	defer rows.Close()
	repos, err := scanReposWithStats(rows)
	return repos, total, err
}

// PublicLanguages lists the primary languages of public repos, most used first.
func (s *ExploreStore) PublicLanguages(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT primary_language FROM repositories
WHERE private = FALSE AND deleted_at IS NULL AND primary_language IS NOT NULL AND primary_language <> ''
GROUP BY primary_language ORDER BY COUNT(*) DESC, primary_language`)
	if err != nil {
		return nil, fmt.Errorf("public languages: %w", err)
	}
	defer rows.Close()
	var langs []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, fmt.Errorf("public languages: %w", err)
		}
		langs = append(langs, l)
	}
	return langs, rows.Err()
}

func scanReposWithStats(rows *sql.Rows) ([]model.RepositoryWithStats, error) {
	var repos []model.RepositoryWithStats
	for rows.Next() {
		var r model.RepositoryWithStats
		var orgID, forkOfID sql.NullInt64
		if err := rows.Scan(
			&r.ID, zeroIfNull{&r.OwnerID}, &r.OwnerName, &orgID, &r.Name, &r.Description, &r.Private,
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
