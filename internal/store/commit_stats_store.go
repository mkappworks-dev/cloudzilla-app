package store

import (
	"context"
	"database/sql"
	"time"
)

type CommitDayCount struct {
	RepoID      int64
	UserID      int64
	Day         time.Time
	CommitCount int
}

type CommitStatsStore struct {
	db *sql.DB
}

func NewCommitStatsStore(database *sql.DB) *CommitStatsStore {
	return &CommitStatsStore{db: database}
}

// Replaces (does not add to) the count. Retries of the same backfill window
// stay idempotent because each call carries the full per-day total.
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

// AddCount increments the per-day count by delta, creating the row when it
// does not yet exist. Used by the push path so successive pushes to a
// single day accumulate rather than overwriting one another.
func (s *CommitStatsStore) AddCount(ctx context.Context, repoID, userID int64, day time.Time, delta int) error {
	const q = `
		INSERT INTO commit_day_counts (repo_id, user_id, day, commit_count, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (repo_id, user_id, day)
		DO UPDATE SET commit_count = commit_day_counts.commit_count + EXCLUDED.commit_count, updated_at = NOW()
	`
	_, err := s.db.ExecContext(ctx, q, repoID, userID, day.UTC().Truncate(24*time.Hour), delta)
	return err
}

// HasRowsForRepoSince returns true when at least one commit_day_counts row
// exists for the repo on or after `since`. Used by the startup backfill to
// avoid re-walking history (and double-counting) on repos already populated
// by a prior backfill or by live pushes.
func (s *CommitStatsStore) HasRowsForRepoSince(ctx context.Context, repoID int64, since time.Time) (bool, error) {
	const q = `SELECT 1 FROM commit_day_counts WHERE repo_id = $1 AND day >= $2 LIMIT 1`
	var one int
	err := s.db.QueryRowContext(ctx, q, repoID, since.UTC().Truncate(24*time.Hour)).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Soft-deleted repos are excluded so a user's contribution heatmap stops
// counting work in repos that have since been deleted by their owner.
func (s *CommitStatsStore) ListForUserSince(ctx context.Context, userID int64, since time.Time) ([]CommitDayCount, error) {
	const q = `
		SELECT c.repo_id, c.user_id, c.day, c.commit_count
		FROM commit_day_counts c
		JOIN repositories r ON r.id = c.repo_id
		WHERE c.user_id = $1 AND c.day >= $2 AND r.deleted_at IS NULL
		ORDER BY c.day ASC
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

// UserID in returned rows is zero — the SUM aggregates across users.
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
