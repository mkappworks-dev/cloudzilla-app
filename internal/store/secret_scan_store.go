package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// SecretScanStore keeps the per-repository history-scan state in repo_secret_scans.
type SecretScanStore struct{ db *sql.DB }

func NewSecretScanStore(db *sql.DB) *SecretScanStore { return &SecretScanStore{db: db} }

// Claim leases one repository whose scan is pending, failed or running under an expired lease, and marks it running.
// A failed scan carries its retry time in lease_until, so it is invisible until then. ok is false when none is due.
func (s *SecretScanStore) Claim(ctx context.Context, lease time.Duration) (repoID int64, ok bool, err error) {
	err = s.db.QueryRowContext(ctx,
		`UPDATE repo_secret_scans SET state = 'running', lease_until = NOW() + make_interval(secs => $1)
		 WHERE repo_id = (
		     SELECT ss.repo_id FROM repo_secret_scans ss JOIN repositories r ON r.id = ss.repo_id
		     WHERE ss.state <> 'done' AND (ss.lease_until IS NULL OR ss.lease_until < NOW()) AND r.deleted_at IS NULL
		     ORDER BY ss.repo_id
		     LIMIT 1
		     FOR UPDATE OF ss SKIP LOCKED)
		 RETURNING repo_id`, lease.Seconds()).Scan(&repoID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("secret scan claim: %w", err)
	}
	return repoID, true, nil
}

// MarkPending queues a fresh scan. One already running finishes first and leaves the row pending.
func (s *SecretScanStore) MarkPending(ctx context.Context, repoID int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO repo_secret_scans (repo_id) VALUES ($1)
		 ON CONFLICT (repo_id) DO UPDATE SET state = 'pending',
		     lease_until = CASE WHEN repo_secret_scans.state = 'running' THEN repo_secret_scans.lease_until END`, repoID)
	if err != nil {
		return fmt.Errorf("secret scan mark pending: %w", err)
	}
	return nil
}

// Finish records a scan's end. A MarkPending that arrived during it keeps the row pending.
func (s *SecretScanStore) Finish(ctx context.Context, repoID int64, incomplete bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE repo_secret_scans SET state = CASE WHEN state = 'pending' THEN 'pending' ELSE 'done' END,
		     incomplete = $2, lease_until = NULL, scanned_at = NOW(), error = ''
		 WHERE repo_id = $1`, repoID, incomplete)
	if err != nil {
		return fmt.Errorf("secret scan finish: %w", err)
	}
	return nil
}

// Fail records a failed scan and keeps it from being claimed again for retryAfter.
func (s *SecretScanStore) Fail(ctx context.Context, repoID int64, msg string, retryAfter time.Duration) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE repo_secret_scans SET state = 'failed', error = $2, lease_until = NOW() + make_interval(secs => $3)
		 WHERE repo_id = $1`, repoID, msg, retryAfter.Seconds())
	if err != nil {
		return fmt.Errorf("secret scan fail: %w", err)
	}
	return nil
}

// State returns the scan state and whether the last scan stopped at its budget. A repository with no row reads as done.
func (s *SecretScanStore) State(ctx context.Context, repoID int64) (state string, incomplete bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT state, incomplete FROM repo_secret_scans WHERE repo_id = $1`, repoID).Scan(&state, &incomplete)
	if errors.Is(err, sql.ErrNoRows) {
		return "done", false, nil
	}
	return state, incomplete, err
}

// HasLocation reports whether the repository already has an alert for hash with this exact location.
func (s *SecretScanStore) HasLocation(ctx context.Context, repoID int64, hash string, loc model.SecretSighting) (bool, error) {
	var found bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM secret_scan_alerts a JOIN secret_scan_locations l ON l.alert_id = a.id
		   WHERE a.repo_id = $1 AND a.secret_hash = $2 AND l.commit_sha = $3 AND l.file_path = $4 AND l.line_number = $5)`,
		repoID, hash, loc.CommitSHA, loc.FilePath, loc.Line).Scan(&found)
	return found, err
}
