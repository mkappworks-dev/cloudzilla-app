package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

var (
	ErrExportActive  = errors.New("an export is already in progress")
	ErrExportTooSoon = errors.New("an export was requested in the last day")
)

// ExportStore keeps the account_exports rows and reads the user-authored data an export holds.
type ExportStore struct{ db *sql.DB }

func NewExportStore(db *sql.DB) *ExportStore { return &ExportStore{db: db} }

const exportColumns = `id, user_id, status, storage_key, error, requested_at, started_at, ready_at, expires_at`

func scanExport(row interface{ Scan(...any) error }) (*model.AccountExport, error) {
	var e model.AccountExport
	if err := row.Scan(&e.ID, &e.UserID, &e.Status, &e.StorageKey, &e.Error, &e.RequestedAt, &e.StartedAt, &e.ReadyAt, &e.ExpiresAt); err != nil {
		return nil, err
	}
	return &e, nil
}

// Create queues an export for userID. The users row lock serializes one user's requests, so the active check and
// the daily limit cannot be raced past. A failed export does not count against the limit, so the user can retry.
func (s *ExportStore) Create(ctx context.Context, userID int64, gap time.Duration) (*model.AccountExport, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("export create begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var locked int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&locked); err != nil {
		return nil, fmt.Errorf("export create lock user: %w", err)
	}
	var active, recent bool
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(bool_or(status IN ('requested', 'running')), false),
		        COALESCE(bool_or(status <> 'failed' AND requested_at > NOW() - make_interval(secs => $2)), false)
		   FROM account_exports WHERE user_id = $1`,
		userID, gap.Seconds(),
	).Scan(&active, &recent); err != nil {
		return nil, fmt.Errorf("export create check: %w", err)
	}
	switch {
	case active:
		return nil, ErrExportActive
	case recent:
		return nil, ErrExportTooSoon
	}
	e, err := scanExport(tx.QueryRowContext(ctx,
		`INSERT INTO account_exports (user_id) VALUES ($1) RETURNING `+exportColumns, userID))
	if err != nil {
		return nil, fmt.Errorf("export create insert: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("export create commit: %w", err)
	}
	return e, nil
}

// Latest is sql.ErrNoRows when userID has never asked.
func (s *ExportStore) Latest(ctx context.Context, userID int64) (*model.AccountExport, error) {
	return scanExport(s.db.QueryRowContext(ctx,
		`SELECT `+exportColumns+` FROM account_exports WHERE user_id = $1 ORDER BY requested_at DESC, id DESC LIMIT 1`, userID))
}

// Get is sql.ErrNoRows for another user's export, so an ID reveals nothing.
func (s *ExportStore) Get(ctx context.Context, userID, id int64) (*model.AccountExport, error) {
	return scanExport(s.db.QueryRowContext(ctx,
		`SELECT `+exportColumns+` FROM account_exports WHERE id = $1 AND user_id = $2`, id, userID))
}

// ClaimNext takes the oldest queued export, or a running one whose lease is older than stale (its worker died).
// It returns sql.ErrNoRows when there is nothing to do.
func (s *ExportStore) ClaimNext(ctx context.Context, stale time.Duration) (*model.AccountExport, error) {
	return scanExport(s.db.QueryRowContext(ctx,
		`UPDATE account_exports SET status = 'running', started_at = NOW()
		  WHERE id = (SELECT id FROM account_exports
		               WHERE status = 'requested'
		                  OR (status = 'running' AND started_at < NOW() - make_interval(secs => $1))
		               ORDER BY requested_at, id
		               LIMIT 1 FOR UPDATE SKIP LOCKED)
		  RETURNING `+exportColumns, stale.Seconds()))
}

// MarkReady reports false when the row is gone, which is how a deleted account shows up to a running worker.
func (s *ExportStore) MarkReady(ctx context.Context, id int64, key string, ttl time.Duration) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE account_exports SET status = 'ready', storage_key = $2, error = '', ready_at = NOW(),
		        expires_at = NOW() + make_interval(secs => $3)
		  WHERE id = $1 AND status = 'running'`, id, key, ttl.Seconds())
	if err != nil {
		return false, fmt.Errorf("export mark ready: %w", err)
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// MarkFailed gives the row an expiry too, so the sweep clears failures.
func (s *ExportStore) MarkFailed(ctx context.Context, id int64, message string, ttl time.Duration) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE account_exports SET status = 'failed', error = $2, expires_at = NOW() + make_interval(secs => $3)
		  WHERE id = $1 AND status = 'running'`, id, message, ttl.Seconds())
	if err != nil {
		return fmt.Errorf("export mark failed: %w", err)
	}
	return nil
}

func (s *ExportStore) ListExpired(ctx context.Context, limit int) ([]model.AccountExport, error) {
	return s.list(ctx,
		`SELECT `+exportColumns+` FROM account_exports WHERE expires_at < NOW() ORDER BY expires_at LIMIT $1`, limit)
}

// ListByUser returns every export of userID, including ones still running.
func (s *ExportStore) ListByUser(ctx context.Context, userID int64) ([]model.AccountExport, error) {
	return s.list(ctx, `SELECT `+exportColumns+` FROM account_exports WHERE user_id = $1 ORDER BY id`, userID)
}

func (s *ExportStore) list(ctx context.Context, query string, args ...any) ([]model.AccountExport, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("export list: %w", err)
	}
	defer rows.Close()
	var out []model.AccountExport
	for rows.Next() {
		e, err := scanExport(rows)
		if err != nil {
			return nil, fmt.Errorf("export list scan: %w", err)
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (s *ExportStore) Delete(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM account_exports WHERE id = $1`, id); err != nil {
		return fmt.Errorf("export delete: %w", err)
	}
	return nil
}
