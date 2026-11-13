package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// CIRunnerStore persists runners and their registration tokens.
type CIRunnerStore struct{ db *sql.DB }

func NewCIRunnerStore(db *sql.DB) *CIRunnerStore { return &CIRunnerStore{db: db} }

const runnerColumns = `id, scope, repo_id, name, labels, version, last_contact_at, revoked_at, created_by, created_at`

func scanRunner(sc interface{ Scan(...any) error }) (*model.CIRunner, error) {
	var r model.CIRunner
	var labels []byte
	if err := sc.Scan(&r.ID, &r.Scope, &r.RepoID, &r.Name, &labels, &r.Version, &r.LastContactAt, &r.RevokedAt,
		&r.CreatedBy, &r.CreatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(labels, &r.Labels); err != nil {
		return nil, fmt.Errorf("decode runner labels: %w", err)
	}
	return &r, nil
}

func (s *CIRunnerStore) InsertRegistrationToken(ctx context.Context, t *model.CIRegistrationToken, hash string) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO ci_runner_registration_tokens (scope, repo_id, token_hash, created_by, expires_at)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		t.Scope, t.RepoID, hash, t.CreatedBy, t.ExpiresAt).Scan(&t.ID, &t.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert runner registration token: %w", err)
	}
	return nil
}

// ConsumeRegistrationToken marks an unused, unexpired token used and returns it; nil when there is none, so two
// runners racing for one token cannot both win.
func (s *CIRunnerStore) ConsumeRegistrationToken(ctx context.Context, hash string) (*model.CIRegistrationToken, error) {
	var t model.CIRegistrationToken
	err := s.db.QueryRowContext(ctx,
		`UPDATE ci_runner_registration_tokens SET used_at = NOW()
		 WHERE token_hash = $1 AND used_at IS NULL AND expires_at > NOW()
		 RETURNING id, scope, repo_id, created_by, created_at, expires_at, used_at`, hash).
		Scan(&t.ID, &t.Scope, &t.RepoID, &t.CreatedBy, &t.CreatedAt, &t.ExpiresAt, &t.UsedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("consume runner registration token: %w", err)
	}
	return &t, nil
}

func (s *CIRunnerStore) InsertRunner(ctx context.Context, r *model.CIRunner, tokenHash string) error {
	labels, err := json.Marshal(nonNilSlice(r.Labels))
	if err != nil {
		return fmt.Errorf("encode runner labels: %w", err)
	}
	err = s.db.QueryRowContext(ctx,
		`INSERT INTO ci_runners (scope, repo_id, name, labels, token_hash, version, created_by, last_contact_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NOW()) RETURNING id, created_at`,
		r.Scope, r.RepoID, r.Name, labels, tokenHash, r.Version, r.CreatedBy).Scan(&r.ID, &r.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert runner: %w", err)
	}
	return nil
}

// RunnerByTokenHash returns the runner with that token hash, revoked or not, or nil.
func (s *CIRunnerStore) RunnerByTokenHash(ctx context.Context, hash string) (*model.CIRunner, error) {
	r, err := scanRunner(s.db.QueryRowContext(ctx, `SELECT `+runnerColumns+` FROM ci_runners WHERE token_hash = $1`, hash))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("runner by token: %w", err)
	}
	return r, nil
}

func (s *CIRunnerStore) GetRunner(ctx context.Context, id int64) (*model.CIRunner, error) {
	r, err := scanRunner(s.db.QueryRowContext(ctx, `SELECT `+runnerColumns+` FROM ci_runners WHERE id = $1`, id))
	if err != nil {
		return nil, fmt.Errorf("get runner: %w", err)
	}
	return r, nil
}

func (s *CIRunnerStore) listRunners(ctx context.Context, where string, args ...any) ([]model.CIRunner, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+runnerColumns+` FROM ci_runners `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list runners: %w", err)
	}
	defer rows.Close()
	var out []model.CIRunner
	for rows.Next() {
		r, err := scanRunner(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (s *CIRunnerStore) ListInstanceRunners(ctx context.Context) ([]model.CIRunner, error) {
	return s.listRunners(ctx, `WHERE scope = 'instance'`)
}

func (s *CIRunnerStore) ListRepoRunners(ctx context.Context, repoID int64) ([]model.CIRunner, error) {
	return s.listRunners(ctx, `WHERE repo_id = $1`, repoID)
}

// Revoke marks the runner revoked; its token stops authenticating on the next request.
func (s *CIRunnerStore) Revoke(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE ci_runners SET revoked_at = COALESCE(revoked_at, NOW()) WHERE id = $1`, id); err != nil {
		return fmt.Errorf("revoke runner: %w", err)
	}
	return nil
}

func (s *CIRunnerStore) TouchRunner(ctx context.Context, id int64, at time.Time) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE ci_runners SET last_contact_at = $2 WHERE id = $1`, id, at); err != nil {
		return fmt.Errorf("touch runner: %w", err)
	}
	return nil
}

// CurrentJobs maps each runner of ids that is running a Job to that Job.
func (s *CIRunnerStore) CurrentJobs(ctx context.Context, ids []int64) (map[int64]model.CIRunnerJob, error) {
	out := map[int64]model.CIRunnerJob{}
	if len(ids) == 0 {
		return out, nil
	}
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		ph[i], args[i] = fmt.Sprintf("$%d", i+1), id
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT j.runner_id, j.name, r.number, repo.owner_name, repo.name
		 FROM workflow_jobs j JOIN workflow_runs r ON r.id = j.run_id JOIN repositories repo ON repo.id = r.repo_id
		 WHERE j.status = 'running' AND j.runner_id IN (`+strings.Join(ph, ",")+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("runner current jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var j model.CIRunnerJob
		if err := rows.Scan(&j.RunnerID, &j.JobName, &j.RunNumber, &j.Owner, &j.Repo); err != nil {
			return nil, err
		}
		out[j.RunnerID] = j
	}
	return out, rows.Err()
}
