package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// MirrorStore persists pull mirrors. Schedule times it sets come from the
// database's clock, so instances with skewed clocks agree on what is due.
type MirrorStore struct {
	db *sql.DB
}

func NewMirrorStore(database *sql.DB) *MirrorStore {
	return &MirrorStore{db: database}
}

const mirrorCols = `m.repo_id, m.remote_url, m.auth_username, m.auth_token_enc, m.interval_seconds, m.next_sync_at,
	m.lease_until, m.last_sync_at, m.last_success_at, m.last_error, m.consecutive_failures, m.created_by,
	m.created_at, m.updated_at, r.owner_name, r.name`

func scanMirror(row interface{ Scan(...any) error }) (*model.RepoMirror, error) {
	var m model.RepoMirror
	var intervalSecs int64
	var lease, lastSync, lastSuccess sql.NullTime
	if err := row.Scan(&m.RepoID, &m.RemoteURL, &m.AuthUsername, &m.AuthTokenEnc, &intervalSecs, &m.NextSyncAt,
		&lease, &lastSync, &lastSuccess, &m.LastError, &m.ConsecutiveFailures, zeroIfNull{&m.CreatedBy},
		&m.CreatedAt, &m.UpdatedAt, &m.OwnerName, &m.RepoName); err != nil {
		return nil, err
	}
	m.Interval = time.Duration(intervalSecs) * time.Second
	m.LeaseUntil = timePtr(lease)
	m.LastSyncAt = timePtr(lastSync)
	m.LastSuccessAt = timePtr(lastSuccess)
	return &m, nil
}

func timePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

// Create schedules the first sync at m.NextSyncAt, or one interval from now when it is zero.
func (s *MirrorStore) Create(ctx context.Context, m *model.RepoMirror) error {
	var next any
	if !m.NextSyncAt.IsZero() {
		next = m.NextSyncAt
	}
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO repo_mirrors (repo_id, remote_url, auth_username, auth_token_enc, interval_seconds, next_sync_at, created_by)
		 VALUES ($1, $2, $3, $4, $5::int, COALESCE($6::timestamptz, NOW() + make_interval(secs => $5::int)), $7)
		 RETURNING next_sync_at, created_at, updated_at`,
		m.RepoID, m.RemoteURL, m.AuthUsername, m.AuthTokenEnc, int64(m.Interval/time.Second), next, nullID(m.CreatedBy),
	).Scan(&m.NextSyncAt, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return fmt.Errorf("mirror create: %w", err)
	}
	return nil
}

// Get wraps sql.ErrNoRows when the repo isn't a mirror.
func (s *MirrorStore) Get(ctx context.Context, repoID int64) (*model.RepoMirror, error) {
	m, err := scanMirror(s.db.QueryRowContext(ctx,
		`SELECT `+mirrorCols+` FROM repo_mirrors m JOIN repositories r ON r.id = m.repo_id WHERE m.repo_id = $1`, repoID))
	if err != nil {
		return nil, fmt.Errorf("mirror get: %w", err)
	}
	return m, nil
}

// MirrorSchedule says how an Update moves the next sync.
type MirrorSchedule int

const (
	MirrorKeepSchedule MirrorSchedule = iota
	// MirrorSyncNow makes the mirror due, and outlives a sync already running.
	MirrorSyncNow
	// MirrorFromLastSync counts the (new) interval from the last sync.
	MirrorFromLastSync
)

// Update saves the remote, credentials and interval, and reschedules per sched.
func (s *MirrorStore) Update(ctx context.Context, m *model.RepoMirror, sched MirrorSchedule) error {
	err := s.db.QueryRowContext(ctx,
		`UPDATE repo_mirrors SET remote_url = $2, auth_username = $3, auth_token_enc = $4, interval_seconds = $5::int,
		        next_sync_at = CASE $6::int
		            WHEN 1 THEN NOW()
		            WHEN 2 THEN GREATEST(NOW(), COALESCE(last_sync_at, NOW()) + make_interval(secs => $5::int))
		            ELSE next_sync_at END,
		        requested_at = CASE WHEN $6::int = 1 THEN NOW() ELSE requested_at END,
		        updated_at = NOW()
		 WHERE repo_id = $1
		 RETURNING next_sync_at`,
		m.RepoID, m.RemoteURL, m.AuthUsername, m.AuthTokenEnc, int64(m.Interval/time.Second), int(sched)).Scan(&m.NextSyncAt)
	if err != nil {
		return fmt.Errorf("mirror update: %w", err)
	}
	return nil
}

func (s *MirrorStore) Delete(ctx context.Context, repoID int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM repo_mirrors WHERE repo_id = $1`, repoID); err != nil {
		return fmt.Errorf("mirror delete: %w", err)
	}
	return nil
}

// DeleteUnleased deletes the mirror unless a sync holds its lease: that sync
// would go on force-fetching into a repo that had just become writable.
func (s *MirrorStore) DeleteUnleased(ctx context.Context, repoID int64) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM repo_mirrors WHERE repo_id = $1 AND (lease_until IS NULL OR lease_until < NOW())`, repoID)
	if err != nil {
		return false, fmt.Errorf("mirror delete: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ReleaseLease gives up a claim without recording a result, leaving the
// mirror due, for a sync cut short by shutdown.
func (s *MirrorStore) ReleaseLease(ctx context.Context, repoID int64) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE repo_mirrors SET lease_until = NULL, next_sync_at = NOW() WHERE repo_id = $1`, repoID); err != nil {
		return fmt.Errorf("mirror release lease: %w", err)
	}
	return nil
}

// ClaimDue leases up to limit due mirrors of live, unarchived repos. A
// leased row is invisible to other claims until the lease ends, so a crashed
// instance's syncs are retried once it expires.
func (s *MirrorStore) ClaimDue(ctx context.Context, limit int, lease time.Duration) ([]model.RepoMirror, error) {
	rows, err := s.db.QueryContext(ctx,
		`UPDATE repo_mirrors m SET lease_until = NOW() + make_interval(secs => $2), claimed_at = NOW()
		 FROM repositories r
		 WHERE r.id = m.repo_id AND m.repo_id IN (
		     SELECT dm.repo_id FROM repo_mirrors dm JOIN repositories dr ON dr.id = dm.repo_id
		     WHERE dm.next_sync_at <= NOW() AND (dm.lease_until IS NULL OR dm.lease_until < NOW())
		       AND dr.deleted_at IS NULL AND NOT dr.is_archived
		     ORDER BY dm.next_sync_at
		     LIMIT $1
		     FOR UPDATE OF dm SKIP LOCKED)
		 RETURNING `+mirrorCols,
		limit, lease.Seconds())
	if err != nil {
		return nil, fmt.Errorf("mirror claim due: %w", err)
	}
	defer rows.Close()
	var claimed []model.RepoMirror
	for rows.Next() {
		m, err := scanMirror(rows)
		if err != nil {
			return nil, fmt.Errorf("mirror claim due: %w", err)
		}
		claimed = append(claimed, *m)
	}
	return claimed, rows.Err()
}

func (s *MirrorStore) RecordSuccess(ctx context.Context, repoID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE repo_mirrors SET last_sync_at = NOW(), last_success_at = NOW(), last_error = '', consecutive_failures = 0,
		        next_sync_at = CASE WHEN `+requestedSinceClaim+` THEN NOW() ELSE NOW() + make_interval(secs => interval_seconds) END,
		        lease_until = NULL, updated_at = NOW()
		 WHERE repo_id = $1`, repoID)
	if err != nil {
		return fmt.Errorf("mirror record success: %w", err)
	}
	return nil
}

// RecordFailure backs off: the first failure waits one interval, and each
// further one doubles it, capped at 24h but never below the interval.
func (s *MirrorStore) RecordFailure(ctx context.Context, repoID int64, msg string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE repo_mirrors SET last_sync_at = NOW(), last_error = $2, consecutive_failures = consecutive_failures + 1,
		        next_sync_at = CASE WHEN `+requestedSinceClaim+` THEN NOW() ELSE NOW() + make_interval(secs => GREATEST(interval_seconds,
		            LEAST(interval_seconds * power(2, LEAST(consecutive_failures, 20)), 86400))) END,
		        lease_until = NULL, updated_at = NOW()
		 WHERE repo_id = $1`, repoID, msg)
	if err != nil {
		return fmt.Errorf("mirror record failure: %w", err)
	}
	return nil
}

// requestedSinceClaim: a sync was asked for while the claimed one ran, so its
// result must leave the mirror due rather than push the next sync out.
const requestedSinceClaim = `requested_at IS NOT NULL AND claimed_at IS NOT NULL AND requested_at > claimed_at`

// MarkDue makes the mirror due now. A sync already holding the lease runs
// on, and the mirror stays due after it.
func (s *MirrorStore) MarkDue(ctx context.Context, repoID int64) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE repo_mirrors SET next_sync_at = NOW(), requested_at = NOW() WHERE repo_id = $1`, repoID); err != nil {
		return fmt.Errorf("mirror mark due: %w", err)
	}
	return nil
}
