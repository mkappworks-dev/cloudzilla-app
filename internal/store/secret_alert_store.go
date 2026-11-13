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

var (
	ErrSecretAlertNotFound = errors.New("secret scan alert not found")
	// ErrSecretAlertState means the alert is not in the state the transition needs (closing a closed alert, say).
	ErrSecretAlertState = errors.New("secret scan alert is not in a state that allows this")
)

type SecretAlertStore struct{ db *sql.DB }

func NewSecretAlertStore(db *sql.DB) *SecretAlertStore { return &SecretAlertStore{db: db} }

const secretAlertColumns = `a.id, a.repo_id, a.number, a.secret_type, a.secret_hash, a.secret_hint, a.state,
	COALESCE(a.resolution, ''), a.resolved_by, a.resolved_at, a.auto_revoked, a.incomplete, a.created_at,
	NOT EXISTS (SELECT 1 FROM secret_scan_locations l WHERE l.alert_id = a.id AND l.reached_ref)`

func scanSecretAlert(row interface{ Scan(...any) error }) (*model.SecretScanAlert, error) {
	var a model.SecretScanAlert
	err := row.Scan(&a.ID, &a.RepoID, &a.Number, &a.SecretType, &a.SecretHash, &a.SecretHint, &a.State,
		&a.Resolution, &a.ResolvedBy, &a.ResolvedAt, &a.AutoRevoked, &a.Incomplete, &a.CreatedAt, &a.Blocked)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func nullActor(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: id != 0} }

// Record stores a sighting. The first sighting of a hash in a repository creates the alert; later ones, open or
// closed, add a location and an event to it. eventKind is 'found' or 'blocked'; a 'blocked' location has not
// reached a ref.
func (s *SecretAlertStore) Record(ctx context.Context, repoID int64, secretType, hash, hint, eventKind string, sg model.SecretSighting) (*model.SecretScanAlert, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	// Serialises numbering and the create-or-append decision per repository, so concurrent sightings of one hash
	// make one alert and concurrent inserts get distinct numbers.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('secret_scan_alerts'), ($1 % 2147483647)::int)`, repoID); err != nil {
		return nil, false, fmt.Errorf("secret alert lock: %w", err)
	}

	var alertID int64
	created := false
	err = tx.QueryRowContext(ctx, `SELECT id FROM secret_scan_alerts WHERE repo_id = $1 AND secret_hash = $2`, repoID, hash).Scan(&alertID)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx,
			`INSERT INTO secret_scan_alerts (repo_id, number, secret_type, secret_hash, secret_hint, incomplete)
			 VALUES ($1, (SELECT COALESCE(MAX(number), 0) + 1 FROM secret_scan_alerts WHERE repo_id = $1), $2, $3, $4, $5)
			 RETURNING id`,
			repoID, secretType, hash, hint, sg.Incomplete).Scan(&alertID)
		created = true
	}
	if err != nil {
		return nil, false, fmt.Errorf("secret alert upsert: %w", err)
	}

	reached := eventKind != model.SecretEventBlocked
	// A later sighting that did reach a ref promotes a location a blocked push left behind.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO secret_scan_locations (alert_id, commit_sha, file_path, line_number, reached_ref)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (alert_id, commit_sha, file_path, line_number)
		 DO UPDATE SET reached_ref = secret_scan_locations.reached_ref OR EXCLUDED.reached_ref`,
		alertID, sg.CommitSHA, sg.FilePath, sg.Line, reached); err != nil {
		return nil, false, fmt.Errorf("secret location insert: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO secret_scan_events (alert_id, kind, actor_id, ref, commit_sha, detail) VALUES ($1, $2, $3, $4, $5, $6)`,
		alertID, eventKind, nullActor(sg.ActorID), sg.Ref, sg.CommitSHA, sg.Detail); err != nil {
		return nil, false, fmt.Errorf("secret event insert: %w", err)
	}
	alert, err := scanSecretAlert(tx.QueryRowContext(ctx, `SELECT `+secretAlertColumns+` FROM secret_scan_alerts a WHERE a.id = $1`, alertID))
	if err != nil {
		return nil, false, fmt.Errorf("secret alert reload: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return alert, created, nil
}

// AppendLocation adds a location with no event, for a push not yet applied; a no-op without an alert for hash.
func (s *SecretAlertStore) AppendLocation(ctx context.Context, repoID int64, hash string, sg model.SecretSighting) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO secret_scan_locations (alert_id, commit_sha, file_path, line_number, reached_ref)
		 SELECT id, $3, $4, $5, FALSE FROM secret_scan_alerts WHERE repo_id = $1 AND secret_hash = $2
		 ON CONFLICT (alert_id, commit_sha, file_path, line_number) DO NOTHING`,
		repoID, hash, sg.CommitSHA, sg.FilePath, sg.Line)
	return err
}

// Close takes eventKind 'closed', 'allowed' or 'auto_revoked'; actorID 0 records no user.
func (s *SecretAlertStore) Close(ctx context.Context, alertID, actorID int64, resolution, eventKind string, autoRevoked bool) error {
	return s.transition(ctx, alertID, model.SecretAlertOpen, eventKind, actorID, resolution,
		`UPDATE secret_scan_alerts SET state = 'closed', resolution = $2, resolved_by = $3, resolved_at = NOW(), auto_revoked = $4
		 WHERE id = $1 AND state = 'open'`, resolution, nullActor(actorID), autoRevoked)
}

func (s *SecretAlertStore) Reopen(ctx context.Context, alertID, actorID int64) error {
	return s.transition(ctx, alertID, model.SecretAlertClosed, model.SecretEventReopened, actorID, "",
		`UPDATE secret_scan_alerts SET state = 'open', resolution = NULL, resolved_by = NULL, resolved_at = NULL, auto_revoked = FALSE
		 WHERE id = $1 AND state = 'closed'`)
}

func (s *SecretAlertStore) transition(ctx context.Context, alertID int64, from, eventKind string, actorID int64, detail, query string, args ...any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, query, append([]any{alertID}, args...)...)
	if err != nil {
		return fmt.Errorf("secret alert %s: %w", eventKind, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var state string
		err := tx.QueryRowContext(ctx, `SELECT state FROM secret_scan_alerts WHERE id = $1`, alertID).Scan(&state)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrSecretAlertNotFound
		}
		if err != nil {
			return err
		}
		return ErrSecretAlertState
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO secret_scan_events (alert_id, kind, actor_id, detail) VALUES ($1, $2, $3, $4)`,
		alertID, eventKind, nullActor(actorID), detail); err != nil {
		return fmt.Errorf("secret event insert: %w", err)
	}
	return tx.Commit()
}

func (s *SecretAlertStore) Get(ctx context.Context, repoID int64, number int) (*model.SecretScanAlert, error) {
	return s.one(ctx, `a.repo_id = $1 AND a.number = $2`, repoID, number)
}

func (s *SecretAlertStore) GetByID(ctx context.Context, alertID int64) (*model.SecretScanAlert, error) {
	return s.one(ctx, `a.id = $1`, alertID)
}

func (s *SecretAlertStore) one(ctx context.Context, where string, args ...any) (*model.SecretScanAlert, error) {
	a, err := scanSecretAlert(s.db.QueryRowContext(ctx, `SELECT `+secretAlertColumns+` FROM secret_scan_alerts a WHERE `+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSecretAlertNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("secret alert get: %w", err)
	}
	return a, nil
}

func (s *SecretAlertStore) IsClosed(ctx context.Context, repoID int64, hash string) (bool, error) {
	var closed bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM secret_scan_alerts WHERE repo_id = $1 AND secret_hash = $2 AND state = 'closed')`,
		repoID, hash).Scan(&closed)
	return closed, err
}

// List is newest first; an empty state or secretType matches all.
func (s *SecretAlertStore) List(ctx context.Context, repoID int64, state, secretType string, page, perPage int) ([]model.SecretScanAlert, int, error) {
	where := `a.repo_id = $1 AND ($2 = '' OR a.state = $2) AND ($3 = '' OR a.secret_type = $3)`
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM secret_scan_alerts a WHERE `+where, repoID, state, secretType).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("secret alert count: %w", err)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+secretAlertColumns+` FROM secret_scan_alerts a WHERE `+where+` ORDER BY a.number DESC LIMIT $4 OFFSET $5`,
		repoID, state, secretType, perPage, max(page-1, 0)*perPage)
	if err != nil {
		return nil, 0, fmt.Errorf("secret alert list: %w", err)
	}
	defer rows.Close()
	var out []model.SecretScanAlert
	for rows.Next() {
		a, err := scanSecretAlert(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *a)
	}
	return out, total, rows.Err()
}

// OpenCount counts open alerts that some ref reaches; a blocked push's alert is not counted.
func (s *SecretAlertStore) OpenCount(ctx context.Context, repoID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM secret_scan_alerts a WHERE a.repo_id = $1 AND a.state = 'open'
		 AND EXISTS (SELECT 1 FROM secret_scan_locations l WHERE l.alert_id = a.id AND l.reached_ref)`, repoID).Scan(&n)
	return n, err
}

func (s *SecretAlertStore) BlockedOpen(ctx context.Context, repoID int64, hashes []string) ([]model.SecretScanAlert, error) {
	if len(hashes) == 0 {
		return nil, nil
	}
	args := []any{repoID}
	holders := make([]string, len(hashes))
	for i, h := range hashes {
		args = append(args, h)
		holders[i] = fmt.Sprintf("$%d", i+2)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+secretAlertColumns+` FROM secret_scan_alerts a WHERE a.repo_id = $1 AND a.state = 'open'
		 AND a.secret_hash IN (`+strings.Join(holders, ",")+`)
		 AND NOT EXISTS (SELECT 1 FROM secret_scan_locations l WHERE l.alert_id = a.id AND l.reached_ref)`, args...)
	if err != nil {
		return nil, fmt.Errorf("secret alert blocked lookup: %w", err)
	}
	defer rows.Close()
	var out []model.SecretScanAlert
	for rows.Next() {
		a, err := scanSecretAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (s *SecretAlertStore) MarkReached(ctx context.Context, repoID int64, commitSHAs []string) error {
	if len(commitSHAs) == 0 {
		return nil
	}
	args := []any{repoID}
	holders := make([]string, len(commitSHAs))
	for i, sha := range commitSHAs {
		args = append(args, sha)
		holders[i] = fmt.Sprintf("$%d", i+2)
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE secret_scan_locations SET reached_ref = TRUE
		 WHERE NOT reached_ref AND commit_sha IN (`+strings.Join(holders, ",")+`)
		 AND alert_id IN (SELECT id FROM secret_scan_alerts WHERE repo_id = $1)`, args...)
	return err
}

// SweepBlocked leaves closed alerts: they are what stops the same value being blocked again.
func (s *SecretAlertStore) SweepBlocked(ctx context.Context, olderThan time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM secret_scan_alerts a WHERE a.state = 'open' AND a.created_at < $1
		 AND NOT EXISTS (SELECT 1 FROM secret_scan_locations l WHERE l.alert_id = a.id AND l.reached_ref)`, olderThan)
	if err != nil {
		return 0, fmt.Errorf("secret alert sweep: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SecretAlertStore) Locations(ctx context.Context, alertID int64) ([]model.SecretScanLocation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, alert_id, commit_sha, file_path, line_number, reached_ref FROM secret_scan_locations WHERE alert_id = $1 ORDER BY id`, alertID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.SecretScanLocation
	for rows.Next() {
		var l model.SecretScanLocation
		if err := rows.Scan(&l.ID, &l.AlertID, &l.CommitSHA, &l.FilePath, &l.LineNumber, &l.ReachedRef); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *SecretAlertStore) Events(ctx context.Context, alertID int64) ([]model.SecretScanEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, alert_id, kind, actor_id, ref, commit_sha, detail, created_at FROM secret_scan_events WHERE alert_id = $1 ORDER BY id`, alertID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.SecretScanEvent
	for rows.Next() {
		var e model.SecretScanEvent
		if err := rows.Scan(&e.ID, &e.AlertID, &e.Kind, &e.ActorID, &e.Ref, &e.CommitSHA, &e.Detail, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
