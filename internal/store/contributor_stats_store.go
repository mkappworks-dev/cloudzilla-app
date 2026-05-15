package store

import (
	"context"
	"database/sql"
	"time"
)

type ContributorWeekStat struct {
	UserID    int64
	Username  string
	Week      time.Time
	Commits   int
	Additions int
	Deletions int
}

type ContributorStatsStore struct{ db *sql.DB }

func NewContributorStatsStore(db *sql.DB) *ContributorStatsStore {
	return &ContributorStatsStore{db: db}
}

func (s *ContributorStatsStore) UpsertStats(ctx context.Context, repoID, userID int64, week time.Time, commits, additions, deletions int) error {
	const q = `
		INSERT INTO contributor_week_stats (repo_id, user_id, week, commits, additions, deletions, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (repo_id, user_id, week)
		DO UPDATE SET commits=EXCLUDED.commits, additions=EXCLUDED.additions, deletions=EXCLUDED.deletions, updated_at=NOW()
	`
	_, err := s.db.ExecContext(ctx, q, repoID, userID, MondayUTC(week), commits, additions, deletions)
	return err
}

func (s *ContributorStatsStore) ListForRepo(ctx context.Context, repoID int64) ([]ContributorWeekStat, error) {
	const q = `
		SELECT c.user_id, u.username, c.week, c.commits, c.additions, c.deletions
		FROM contributor_week_stats c JOIN users u ON u.id = c.user_id
		WHERE c.repo_id = $1
		ORDER BY c.week ASC, u.username
	`
	rows, err := s.db.QueryContext(ctx, q, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ContributorWeekStat
	for rows.Next() {
		var r ContributorWeekStat
		if err := rows.Scan(&r.UserID, &r.Username, &r.Week, &r.Commits, &r.Additions, &r.Deletions); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetOne returns the aggregate row for (repoID, userID, week). Returns zero value on miss.
func (s *ContributorStatsStore) GetOne(ctx context.Context, repoID, userID int64, week time.Time) (ContributorWeekStat, error) {
	const q = `
		SELECT c.user_id, u.username, c.week, c.commits, c.additions, c.deletions
		FROM contributor_week_stats c JOIN users u ON u.id = c.user_id
		WHERE c.repo_id = $1 AND c.user_id = $2 AND c.week = $3
	`
	var r ContributorWeekStat
	err := s.db.QueryRowContext(ctx, q, repoID, userID, MondayUTC(week)).Scan(
		&r.UserID, &r.Username, &r.Week, &r.Commits, &r.Additions, &r.Deletions,
	)
	if err == sql.ErrNoRows {
		return ContributorWeekStat{}, nil
	}
	if err != nil {
		return ContributorWeekStat{}, err
	}
	return r, nil
}

// MondayUTC normalizes t to the Monday of its ISO week in UTC.
func MondayUTC(t time.Time) time.Time {
	t = t.UTC().Truncate(24 * time.Hour)
	for t.Weekday() != time.Monday {
		t = t.AddDate(0, 0, -1)
	}
	return t
}
