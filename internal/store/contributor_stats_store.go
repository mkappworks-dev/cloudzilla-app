package store

import (
	"context"
	"database/sql"
	"errors"
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

// Additive on conflict so concurrent pushes to the same week accumulate without a read-modify-write race.
func (s *ContributorStatsStore) AddDelta(ctx context.Context, repoID, userID int64, week time.Time, commits, additions, deletions int) error {
	const q = `
		INSERT INTO contributor_week_stats (repo_id, user_id, week, commits, additions, deletions, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (repo_id, user_id, week)
		DO UPDATE SET
			commits=contributor_week_stats.commits + EXCLUDED.commits,
			additions=contributor_week_stats.additions + EXCLUDED.additions,
			deletions=contributor_week_stats.deletions + EXCLUDED.deletions,
			updated_at=NOW()
	`
	_, err := s.db.ExecContext(ctx, q, repoID, userID, MondayUTC(week), commits, additions, deletions)
	return err
}

func (s *ContributorStatsStore) AttemptIngest(ctx context.Context, repoID int64, sha string, userID int64, week time.Time, additions, deletions int) (bool, error) {
	const q = `
		INSERT INTO contributor_commits_ingested (repo_id, sha, user_id, week, additions, deletions)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (repo_id, sha) DO NOTHING
		RETURNING 1
	`
	var one int
	err := s.db.QueryRowContext(ctx, q, repoID, sha, userID, MondayUTC(week), additions, deletions).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
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

func MondayUTC(t time.Time) time.Time {
	t = t.UTC().Truncate(24 * time.Hour)
	for t.Weekday() != time.Monday {
		t = t.AddDate(0, 0, -1)
	}
	return t
}

func clampWeeks(weeks int) int {
	if weeks < 1 {
		return 1
	}
	if weeks > 104 {
		return 104
	}
	return weeks
}
