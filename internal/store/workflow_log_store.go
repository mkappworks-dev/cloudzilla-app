package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

var (
	// ErrLogConflict means another chunk of the Job committed first, so the line offsets the caller computed are stale.
	ErrLogConflict = errors.New("job log changed under the write")
	// ErrLogChunkExists means a chunk with this sequence number is already stored.
	ErrLogChunkExists = errors.New("log chunk already stored")

	ErrArtifactLimit     = errors.New("run artifact limit reached")
	ErrArtifactNameTaken = errors.New("job already has an artifact with this name")
)

// WorkflowLogStore persists log chunks and artifacts. It holds no foreign keys to Jobs or repos so that the sweep can
// find the rows of a repo removed by any path.
type WorkflowLogStore struct{ db *sql.DB }

func NewWorkflowLogStore(db *sql.DB) *WorkflowLogStore { return &WorkflowLogStore{db: db} }

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (s *WorkflowLogStore) State(ctx context.Context, jobID int64) (model.WorkflowLogState, error) {
	var st model.WorkflowLogState
	err := s.db.QueryRowContext(ctx, `SELECT log_bytes, log_lines, log_truncated FROM workflow_jobs WHERE id = $1`, jobID).
		Scan(&st.Bytes, &st.Lines, &st.Truncated)
	if err != nil {
		return st, fmt.Errorf("workflow log state: %w", err)
	}
	return st, nil
}

func (s *WorkflowLogStore) ChunkExists(ctx context.Context, jobID int64, seq int) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM workflow_log_chunks WHERE job_id = $1 AND seq = $2`, jobID, seq).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// AddChunk stores c and advances the Job's counters, but only if the Job still has exactly c.FirstLine lines: a
// concurrent writer would otherwise give two chunks the same lines. step is the Step the lines belong to, or -1.
func (s *WorkflowLogStore) AddChunk(ctx context.Context, c *model.WorkflowLogChunk, step int, truncated bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("log chunk begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`UPDATE workflow_jobs SET log_bytes = log_bytes + $2, log_lines = log_lines + $3, log_truncated = log_truncated OR $4
		 WHERE id = $1 AND log_lines = $5`, c.JobID, c.SizeBytes, c.LineCount, truncated, c.FirstLine)
	if err != nil {
		return fmt.Errorf("log chunk counters: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrLogConflict
	}
	err = tx.QueryRowContext(ctx,
		`INSERT INTO workflow_log_chunks (job_id, repo_id, seq, first_line, line_count, size_bytes, storage_key)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, created_at`,
		c.JobID, c.RepoID, c.Seq, c.FirstLine, c.LineCount, c.SizeBytes, c.StorageKey).Scan(&c.ID, &c.CreatedAt)
	if isUnique(err) {
		return ErrLogChunkExists
	}
	if err != nil {
		return fmt.Errorf("log chunk insert: %w", err)
	}
	if step >= 0 {
		if _, err := tx.ExecContext(ctx,
			`UPDATE workflow_steps SET
			   log_first_line = CASE WHEN log_last_line = log_first_line THEN $3 ELSE log_first_line END,
			   log_last_line = $4
			 WHERE job_id = $1 AND position = $2`, c.JobID, step, c.FirstLine, c.FirstLine+c.LineCount); err != nil {
			return fmt.Errorf("log chunk step range: %w", err)
		}
	}
	return tx.Commit()
}

// ChunksBetween lists the chunks holding any line of [from, to), in line order.
func (s *WorkflowLogStore) ChunksBetween(ctx context.Context, jobID int64, from, to int) ([]model.WorkflowLogChunk, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, job_id, repo_id, seq, first_line, line_count, size_bytes, storage_key, created_at
		 FROM workflow_log_chunks WHERE job_id = $1 AND first_line < $3 AND first_line + line_count > $2
		 ORDER BY first_line`, jobID, from, to)
	if err != nil {
		return nil, fmt.Errorf("log chunks: %w", err)
	}
	defer rows.Close()
	var out []model.WorkflowLogChunk
	for rows.Next() {
		var c model.WorkflowLogChunk
		if err := rows.Scan(&c.ID, &c.JobID, &c.RepoID, &c.Seq, &c.FirstLine, &c.LineCount, &c.SizeBytes, &c.StorageKey, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CreateArtifact refuses past max artifacts for the Job's attempt of the Run. It locks the Run row so two concurrent
// uploads can't both pass the count.
func (s *WorkflowLogStore) CreateArtifact(ctx context.Context, a *model.WorkflowArtifact, max int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("artifact begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var locked int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM workflow_runs WHERE id = $1 FOR UPDATE`, a.RunID).Scan(&locked); err != nil {
		return fmt.Errorf("artifact lock run: %w", err)
	}
	var n int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workflow_artifacts a JOIN workflow_jobs j ON j.id = a.job_id
		 WHERE a.run_id = $1 AND j.attempt = (SELECT attempt FROM workflow_jobs WHERE id = $2)`, a.RunID, a.JobID).Scan(&n); err != nil {
		return fmt.Errorf("artifact count: %w", err)
	}
	if n >= max {
		return ErrArtifactLimit
	}
	err = tx.QueryRowContext(ctx,
		`INSERT INTO workflow_artifacts (run_id, job_id, repo_id, name, size, sha256, storage_key, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id, created_at`,
		a.RunID, a.JobID, a.RepoID, a.Name, a.Size, a.SHA256, a.StorageKey, a.ExpiresAt).Scan(&a.ID, &a.CreatedAt)
	if isUnique(err) {
		return ErrArtifactNameTaken
	}
	if err != nil {
		return fmt.Errorf("artifact insert: %w", err)
	}
	return tx.Commit()
}

func (s *WorkflowLogStore) ArtifactCount(ctx context.Context, runID, jobID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workflow_artifacts a JOIN workflow_jobs j ON j.id = a.job_id
		 WHERE a.run_id = $1 AND j.attempt = (SELECT attempt FROM workflow_jobs WHERE id = $2)`, runID, jobID).Scan(&n)
	return n, err
}

const artifactColumns = `id, run_id, job_id, repo_id, name, size, sha256, storage_key, created_at, expires_at`

func scanArtifact(sc interface{ Scan(...any) error }) (*model.WorkflowArtifact, error) {
	var a model.WorkflowArtifact
	if err := sc.Scan(&a.ID, &a.RunID, &a.JobID, &a.RepoID, &a.Name, &a.Size, &a.SHA256, &a.StorageKey, &a.CreatedAt, &a.ExpiresAt); err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *WorkflowLogStore) GetArtifact(ctx context.Context, id int64) (*model.WorkflowArtifact, error) {
	return scanArtifact(s.db.QueryRowContext(ctx, `SELECT `+artifactColumns+` FROM workflow_artifacts WHERE id = $1`, id))
}

func (s *WorkflowLogStore) ListArtifacts(ctx context.Context, runID int64) ([]model.WorkflowArtifact, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+artifactColumns+` FROM workflow_artifacts WHERE run_id = $1 ORDER BY id`, runID)
	if err != nil {
		return nil, fmt.Errorf("artifact list: %w", err)
	}
	defer rows.Close()
	var out []model.WorkflowArtifact
	for rows.Next() {
		a, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// A delete method returns the object keys of the rows it removed: the caller deletes the objects after this commits.

// DeleteExpiredChunks removes up to limit chunks created before cutoff.
func (s *WorkflowLogStore) DeleteExpiredChunks(ctx context.Context, cutoff time.Time, limit int) ([]string, error) {
	return s.deleteKeys(ctx,
		`DELETE FROM workflow_log_chunks WHERE id IN (SELECT id FROM workflow_log_chunks WHERE created_at < $1 LIMIT $2) RETURNING storage_key`,
		cutoff, limit)
}

func (s *WorkflowLogStore) DeleteExpiredArtifacts(ctx context.Context, now time.Time, limit int) ([]string, error) {
	return s.deleteKeys(ctx,
		`DELETE FROM workflow_artifacts WHERE id IN (SELECT id FROM workflow_artifacts WHERE expires_at < $1 LIMIT $2) RETURNING storage_key`,
		now, limit)
}

// DeleteOrphanChunks removes up to limit chunks whose Job or repo no longer exists.
func (s *WorkflowLogStore) DeleteOrphanChunks(ctx context.Context, limit int) ([]string, error) {
	return s.deleteKeys(ctx,
		`DELETE FROM workflow_log_chunks WHERE id IN (SELECT c.id FROM workflow_log_chunks c
		   WHERE NOT EXISTS (SELECT 1 FROM workflow_jobs j WHERE j.id = c.job_id)
		      OR NOT EXISTS (SELECT 1 FROM repositories r WHERE r.id = c.repo_id) LIMIT $1) RETURNING storage_key`, limit)
}

func (s *WorkflowLogStore) DeleteOrphanArtifacts(ctx context.Context, limit int) ([]string, error) {
	return s.deleteKeys(ctx,
		`DELETE FROM workflow_artifacts WHERE id IN (SELECT a.id FROM workflow_artifacts a
		   WHERE NOT EXISTS (SELECT 1 FROM workflow_jobs j WHERE j.id = a.job_id)
		      OR NOT EXISTS (SELECT 1 FROM repositories r WHERE r.id = a.repo_id) LIMIT $1) RETURNING storage_key`, limit)
}

// DeleteForRepo removes every chunk and artifact row of a repo, for a purge.
func (s *WorkflowLogStore) DeleteForRepo(ctx context.Context, repoID int64) ([]string, error) {
	chunks, err := s.deleteKeys(ctx, `DELETE FROM workflow_log_chunks WHERE repo_id = $1 RETURNING storage_key`, repoID)
	if err != nil {
		return nil, err
	}
	arts, err := s.deleteKeys(ctx, `DELETE FROM workflow_artifacts WHERE repo_id = $1 RETURNING storage_key`, repoID)
	return append(chunks, arts...), err
}

func (s *WorkflowLogStore) deleteKeys(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("workflow log delete: %w", err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}
