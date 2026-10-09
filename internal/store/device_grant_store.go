package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

var ErrTooManyGrants = errors.New("too many live device grants")

// DeviceGrantStore provides database operations for device-code login grants.
type DeviceGrantStore struct{ db *sql.DB }

func NewDeviceGrantStore(db *sql.DB) *DeviceGrantStore { return &DeviceGrantStore{db: db} }

const deviceGrantColumns = `id, device_code_hash, user_code, scopes, device_name, status, user_id, requester_ip, interval_secs, last_polled_at, expires_at, created_at`

func scanDeviceGrant(row rowScanner) (*model.DeviceGrant, error) {
	g := &model.DeviceGrant{}
	var scopes string
	if err := row.Scan(&g.ID, &g.DeviceCodeHash, &g.UserCode, &scopes, &g.DeviceName, &g.Status, &g.UserID,
		&g.RequesterIP, &g.IntervalSecs, &g.LastPolledAt, &g.ExpiresAt, &g.CreatedAt); err != nil {
		return nil, err
	}
	if scopes != "" {
		g.Scopes = strings.Split(scopes, ",")
	}
	return g, nil
}

// Create inserts g unless requester_ip already holds maxLive unexpired grants. The count and
// the insert are one statement, but two concurrent requests can both pass it; the per-hour
// request limiter on the route bounds that overshoot.
func (s *DeviceGrantStore) Create(ctx context.Context, g *model.DeviceGrant, maxLive int, now time.Time) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO device_grants (device_code_hash, user_code, scopes, device_name, requester_ip, interval_secs, expires_at)
		 SELECT $1, $2, $3, $4, $5, $6, $7
		 WHERE (SELECT COUNT(*) FROM device_grants
		        WHERE requester_ip = $5 AND status IN ('pending', 'approved') AND expires_at > $8) < $9
		 RETURNING id, created_at`,
		g.DeviceCodeHash, g.UserCode, strings.Join(g.Scopes, ","), g.DeviceName, g.RequesterIP, g.IntervalSecs, g.ExpiresAt, now, maxLive,
	).Scan(&g.ID, &g.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTooManyGrants
	}
	if err != nil {
		return fmt.Errorf("device grant create: %w", err)
	}
	return nil
}

func (s *DeviceGrantStore) GetByDeviceHash(ctx context.Context, hash string) (*model.DeviceGrant, error) {
	return scanDeviceGrant(s.db.QueryRowContext(ctx,
		`SELECT `+deviceGrantColumns+` FROM device_grants WHERE device_code_hash = $1`, hash))
}

func (s *DeviceGrantStore) GetPendingByUserCode(ctx context.Context, userCode string, now time.Time) (*model.DeviceGrant, error) {
	return scanDeviceGrant(s.db.QueryRowContext(ctx,
		`SELECT `+deviceGrantColumns+` FROM device_grants WHERE user_code = $1 AND status = 'pending' AND expires_at > $2`, userCode, now))
}

// Approve records userID's decision and the scopes they kept. Only a pending, unexpired
// grant changes, so a second click or a late one is sql.ErrNoRows.
func (s *DeviceGrantStore) Approve(ctx context.Context, userCode string, userID int64, scopes []string, now time.Time) error {
	return s.decide(ctx, userCode, userID, model.DeviceGrantApproved, strings.Join(scopes, ","), now)
}

func (s *DeviceGrantStore) Deny(ctx context.Context, userCode string, userID int64, now time.Time) error {
	return s.decide(ctx, userCode, userID, model.DeviceGrantDenied, "", now)
}

func (s *DeviceGrantStore) decide(ctx context.Context, userCode string, userID int64, status, scopes string, now time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE device_grants SET status = $1, user_id = $2, scopes = CASE WHEN $3 = '' THEN scopes ELSE $3 END
		 WHERE user_code = $4 AND status = 'pending' AND expires_at > $5`,
		status, userID, scopes, userCode, now)
	if err != nil {
		return fmt.Errorf("device grant decide: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Touch records a poll and reports whether it came before last_polled_at plus the interval.
// An early poll also lengthens the interval by 5 s, as RFC 8628 §3.5 asks of slow_down.
func (s *DeviceGrantStore) Touch(ctx context.Context, id int64, now time.Time) (bool, error) {
	var tooFast bool
	err := s.db.QueryRowContext(ctx,
		`WITH prev AS (
		     SELECT id, interval_secs,
		            (last_polled_at IS NOT NULL AND $2::timestamptz < last_polled_at + make_interval(secs => interval_secs::double precision)) AS fast
		     FROM device_grants WHERE id = $1 FOR UPDATE
		 )
		 UPDATE device_grants g
		 SET last_polled_at = $2, interval_secs = prev.interval_secs + CASE WHEN prev.fast THEN 5 ELSE 0 END
		 FROM prev WHERE g.id = prev.id
		 RETURNING prev.fast`, id, now).Scan(&tooFast)
	if err != nil {
		return false, fmt.Errorf("device grant touch: %w", err)
	}
	return tooFast, nil
}

// Redeem claims an approved, unexpired grant for good and runs insert in the same
// transaction, so a token exists exactly when the grant is consumed. A concurrent second
// Redeem waits on the row lock, then finds the grant consumed and gets sql.ErrNoRows.
func (s *DeviceGrantStore) Redeem(ctx context.Context, deviceHash string, now time.Time,
	insert func(ctx context.Context, tx *sql.Tx, g *model.DeviceGrant) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("device grant redeem: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	g, err := scanDeviceGrant(tx.QueryRowContext(ctx,
		`UPDATE device_grants SET status = 'consumed'
		 WHERE device_code_hash = $1 AND status = 'approved' AND expires_at > $2
		 RETURNING `+deviceGrantColumns, deviceHash, now))
	if err != nil {
		return err
	}
	if err := insert(ctx, tx, g); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *DeviceGrantStore) DeleteStale(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM device_grants WHERE created_at < $1`, before)
	return err
}
