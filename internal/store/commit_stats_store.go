package store

import (
	"context"
	"database/sql"
	"time"
)

// CommitDayCount is one row of commit_day_counts. RepoID is informational;
// ListForRepoSince zeroes UserID because the SUM aggregates across users.
type CommitDayCount struct {
	RepoID      int64
	UserID      int64
	Day         time.Time
	CommitCount int
}

// CommitStatsStore provides database operations for the
// commit_day_counts aggregate table that backs the contribution heatmap.
type CommitStatsStore struct {
	db *sql.DB
}

// NewCommitStatsStore creates a CommitStatsStore backed by the given database.
func NewCommitStatsStore(database *sql.DB) *CommitStatsStore {
	return &CommitStatsStore{db: database}
}

// UpsertCount replaces (does not add to) the commit count for the
// (repo, user, day) tuple. Idempotent for retry of the same push.
func (s *CommitStatsStore) UpsertCount(ctx context.Context, repoID, userID int64, day time.Time, count int) error {
	const q = `
		INSERT INTO commit_day_counts (repo_id, user_id, day, commit_count, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (repo_id, user_id, day)
		DO UPDATE SET commit_count = EXCLUDED.commit_count, updated_at = NOW()
	`
	_, err := s.db.ExecContext(ctx, q, repoID, userID, day.UTC().Truncate(24*time.Hour), count)
	return err
}

// ListForUserSince returns per-day commit counts for the user across all
// their repos since `since` (inclusive), ordered by day ascending.
func (s *CommitStatsStore) ListForUserSince(ctx context.Context, userID int64, since time.Time) ([]CommitDayCount, error) {
	const q = `
		SELECT repo_id, user_id, day, commit_count
		FROM commit_day_counts
		WHERE user_id = $1 AND day >= $2
		ORDER BY day ASC
	`
	rows, err := s.db.QueryContext(ctx, q, userID, since.UTC().Truncate(24*time.Hour))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CommitDayCount
	for rows.Next() {
		var c CommitDayCount
		if err := rows.Scan(&c.RepoID, &c.UserID, &c.Day, &c.CommitCount); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListForRepoSince returns per-day commit counts (summed across users) for
// one repo. UserID in the returned rows is zero because the SUM crosses users.
func (s *CommitStatsStore) ListForRepoSince(ctx context.Context, repoID int64, since time.Time) ([]CommitDayCount, error) {
	const q = `
		SELECT day, SUM(commit_count)::int AS commit_count
		FROM commit_day_counts
		WHERE repo_id = $1 AND day >= $2
		GROUP BY day
		ORDER BY day ASC
	`
	rows, err := s.db.QueryContext(ctx, q, repoID, since.UTC().Truncate(24*time.Hour))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CommitDayCount
	for rows.Next() {
		c := CommitDayCount{RepoID: repoID}
		if err := rows.Scan(&c.Day, &c.CommitCount); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
