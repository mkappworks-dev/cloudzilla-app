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

// Replace semantics. Test-only — production uses AddCount; mixing the two on one bucket produces wrong totals.
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

// Additive on conflict so successive pushes to the same day accumulate.
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

// Backfill uses this to skip repos already populated and avoid double-counting against AddCount.
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

// Excludes soft-deleted repos so the heatmap stops counting work the user can no longer browse.
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

func (s *CommitStatsStore) WeeklyForRepo(ctx context.Context, repoID int64, weeks int) ([]int, error) {
	weeks = clampWeeks(weeks)
	const q = `
		WITH w AS (
			SELECT generate_series(
				date_trunc('week', (NOW() AT TIME ZONE 'UTC') - ($2::int - 1) * interval '1 week'),
				date_trunc('week', (NOW() AT TIME ZONE 'UTC')),
				interval '1 week'
			) AS ws
		)
		SELECT COALESCE(SUM(c.commit_count), 0)::int
		FROM w
		LEFT JOIN commit_day_counts c ON date_trunc('week', c.day AT TIME ZONE 'UTC') = w.ws AND c.repo_id = $1
		GROUP BY w.ws
		ORDER BY w.ws ASC
	`
	rows, err := s.db.QueryContext(ctx, q, repoID, weeks)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
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
