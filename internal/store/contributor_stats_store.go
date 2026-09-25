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

// dbtx lets one query body serve both autocommit (*sql.DB) and transactional (*sql.Tx) callers.
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Reports whether this caller claimed the SHA; a false return means another writer already counted it.
func attemptIngest(ctx context.Context, db dbtx, repoID, userID int64, sha string, week time.Time, additions, deletions int) (bool, error) {
	const q = `
		INSERT INTO commits_ingested (repo_id, sha, user_id, week, additions, deletions)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (repo_id, sha) DO NOTHING
		RETURNING 1
	`
	var one int
	err := db.QueryRowContext(ctx, q, repoID, sha, userID, week, additions, deletions).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Additive on conflict so concurrent pushes to the same week accumulate without a read-modify-write race.
func addDelta(ctx context.Context, db dbtx, repoID, userID int64, week time.Time, commits, additions, deletions int) error {
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
	_, err := db.ExecContext(ctx, q, repoID, userID, week, commits, additions, deletions)
	return err
}

func (s *ContributorStatsStore) AddDelta(ctx context.Context, repoID, userID int64, week time.Time, commits, additions, deletions int) error {
	return addDelta(ctx, s.db, repoID, userID, MondayUTC(week), commits, additions, deletions)
}

// The SHA claim and both aggregate increments share one transaction, so a crash between them can't leave a claimed-but-uncounted commit.
func (s *ContributorStatsStore) IngestCommitTx(ctx context.Context, repoID, userID int64, sha string, when time.Time, additions, deletions int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := ingestCommit(ctx, tx, repoID, CommitIngestRow{UserID: userID, SHA: sha, When: when, Additions: additions, Deletions: deletions}); err != nil {
		return err
	}
	return tx.Commit()
}

type CommitIngestRow struct {
	UserID    int64
	SHA       string
	When      time.Time
	Additions int
	Deletions int
}

func ingestCommit(ctx context.Context, db dbtx, repoID int64, r CommitIngestRow) error {
	week := MondayUTC(r.When)
	claimed, err := attemptIngest(ctx, db, repoID, r.UserID, r.SHA, week, r.Additions, r.Deletions)
	if err != nil || !claimed {
		return err
	}
	if err := addDelta(ctx, db, repoID, r.UserID, week, 1, r.Additions, r.Deletions); err != nil {
		return err
	}
	return addCount(ctx, db, repoID, r.UserID, r.When, 1)
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
