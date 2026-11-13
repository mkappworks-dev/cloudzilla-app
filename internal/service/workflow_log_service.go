package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/attachment"
	"github.com/mkappworks-dev/cloudzilla-app/internal/ci/logmask"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

var (
	ErrArtifactNotFound  = errors.New("artifact not found")
	ErrArtifactTooLarge  = errors.New("artifact is too large")
	ErrArtifactLimit     = errors.New("run has too many artifacts")
	ErrArtifactNameTaken = errors.New("job already has an artifact with this name")
	ErrArtifactName      = errors.New("artifact name is empty")
)

// LimitError carries a refusal that names the limit and the config key that sets it, so the runner log says which
// knob to turn. errors.Is matches the sentinel it wraps.
type LimitError struct {
	Err error
	Msg string
}

func (e *LimitError) Error() string { return e.Msg }
func (e *LimitError) Unwrap() error { return e.Err }

const (
	logSweepEvery = time.Hour
	sweepBatch    = 500
	// logAppendTries bounds the retries after another chunk of the Job committed between the read and the write.
	logAppendTries = 3
	// MaxLogLineLimit caps the lines one ReadLog returns.
	MaxLogLineLimit = 5000
	DefaultLogLimit = 1000
)

// SecretSource yields the secret values of a repository for the log backstop. A nil source masks nothing.
type SecretSource interface {
	SecretValues(ctx context.Context, repoID int64) ([]string, error)
}

// WorkflowLogService keeps Job logs and artifacts. The storage Backend has no append, so a log is a series of chunk
// objects, each with a row giving its line range. It does not decide who may read: callers check the repo first.
type WorkflowLogService struct {
	logs    *store.WorkflowLogStore
	backend storage.Backend
	cfg     config.CIConfig
	secrets SecretSource
	maskers sync.Map // job id -> maskEntry
}

func NewWorkflowLogService(logs *store.WorkflowLogStore, cfg config.CIConfig) *WorkflowLogService {
	d := config.DefaultCI()
	if cfg.MaxLogBytes <= 0 {
		cfg.MaxLogBytes = d.MaxLogBytes
	}
	if cfg.MaxArtifactBytes <= 0 {
		cfg.MaxArtifactBytes = d.MaxArtifactBytes
	}
	if cfg.MaxArtifactsPerRun <= 0 {
		cfg.MaxArtifactsPerRun = d.MaxArtifactsPerRun
	}
	if cfg.LogRetention <= 0 {
		cfg.LogRetention = d.LogRetention
	}
	if cfg.ArtifactRetention <= 0 {
		cfg.ArtifactRetention = d.ArtifactRetention
	}
	return &WorkflowLogService{logs: logs, cfg: cfg}
}

// WithBackend must be called before the service handles a request.
func (s *WorkflowLogService) WithBackend(b storage.Backend) *WorkflowLogService {
	s.backend = b
	return s
}

// WithSecrets sets where the server-side masking backstop gets a repo's secret values.
func (s *WorkflowLogService) WithSecrets(src SecretSource) *WorkflowLogService {
	s.secrets = src
	return s
}

func (s *WorkflowLogService) MaxArtifactBytes() int64 { return s.cfg.MaxArtifactBytes }

func humanBytes(n int64) string {
	if n >= 1<<20 && n%(1<<20) == 0 {
		return fmt.Sprintf("%d MiB", n>>20)
	}
	return fmt.Sprintf("%d bytes", n)
}

func (s *WorkflowLogService) truncationLine() string {
	return fmt.Sprintf("[log truncated: this Job reached the %s limit (ci.max_log_bytes)]\n", humanBytes(s.cfg.MaxLogBytes))
}

// maskerTTL bounds how long a Job's masker is reused; Jobs run for hours at most, and a stale entry only lingers.
const maskerTTL = time.Hour

type maskEntry struct {
	m   *logmask.Masker
	exp time.Time
}

// mask hides the Job's repo secrets in data. The masker is built once per Job: loading and decrypting every secret
// for each chunk POST would repeat for the life of the Job.
func (s *WorkflowLogService) mask(ctx context.Context, tok *JobToken, data []byte) ([]byte, error) {
	if s.secrets == nil || tok.NoSecrets {
		return data, nil
	}
	now := time.Now()
	if v, ok := s.maskers.Load(tok.JobID); ok && now.Before(v.(maskEntry).exp) {
		return v.(maskEntry).m.MaskBytes(data), nil
	}
	values, err := s.secrets.SecretValues(ctx, tok.RepoID)
	if err != nil {
		return nil, fmt.Errorf("log masking: %w", err)
	}
	s.maskers.Range(func(k, v any) bool {
		if !now.Before(v.(maskEntry).exp) {
			s.maskers.Delete(k)
		}
		return true
	})
	m := logmask.New(values)
	s.maskers.Store(tok.JobID, maskEntry{m: m, exp: now.Add(maskerTTL)})
	return m.MaskBytes(data), nil
}

// AppendLog stores data, whole lines of Step step (-1 for none), as chunk number seq of the Job. A seq already stored
// is a retried POST and succeeds without storing again. Past the Job's byte cap the chunk is cut at a line boundary
// and a truncation line ends the log; later chunks are dropped. It reports whether the log is truncated.
func (s *WorkflowLogService) AppendLog(ctx context.Context, tok *JobToken, step, seq int, data []byte) (bool, error) {
	if s.backend == nil {
		return false, ErrStorageUnconfigured
	}
	if len(data) == 0 {
		return false, nil
	}
	data, err := s.mask(ctx, tok, data)
	if err != nil {
		return false, err
	}
	if data[len(data)-1] != '\n' {
		data = append(data[:len(data):len(data)], '\n')
	}
	for range logAppendTries {
		truncated, err := s.appendOnce(ctx, tok, step, seq, data)
		if !errors.Is(err, store.ErrLogConflict) {
			return truncated, err
		}
	}
	return false, store.ErrLogConflict
}

func (s *WorkflowLogService) appendOnce(ctx context.Context, tok *JobToken, step, seq int, data []byte) (bool, error) {
	if dup, err := s.logs.ChunkExists(ctx, tok.JobID, seq); err != nil || dup {
		return false, err
	}
	st, err := s.logs.State(ctx, tok.JobID)
	if err != nil {
		return false, err
	}
	if st.Truncated {
		return true, nil
	}
	truncated := false
	if room := s.cfg.MaxLogBytes - st.Bytes; int64(len(data)) > room {
		keep := 0
		if room > 0 {
			keep = bytes.LastIndexByte(data[:room], '\n') + 1
		}
		data = append(data[:keep:keep], s.truncationLine()...)
		truncated = true
	}
	c := &model.WorkflowLogChunk{
		JobID: tok.JobID, RepoID: tok.RepoID, Seq: seq, FirstLine: st.Lines,
		LineCount: bytes.Count(data, []byte{'\n'}), SizeBytes: len(data),
		StorageKey: fmt.Sprintf("ci/logs/repo/%d/job/%d/%d-%s.log", tok.RepoID, tok.JobID, seq, attachment.NewToken()),
	}
	if err := s.backend.Put(ctx, c.StorageKey, bytes.NewReader(data), int64(len(data))); err != nil {
		return false, fmt.Errorf("store log chunk: %w", err)
	}
	if err := s.logs.AddChunk(ctx, c, step, truncated); err != nil {
		s.deleteObject(ctx, c.StorageKey)
		if errors.Is(err, store.ErrLogChunkExists) {
			return false, nil
		}
		return false, err
	}
	return truncated, nil
}

// ReadLog returns up to limit lines from line offset. A chunk whose object has expired ends the page early.
func (s *WorkflowLogService) ReadLog(ctx context.Context, jobID int64, offset, limit int) (*model.WorkflowLogPage, error) {
	if s.backend == nil {
		return nil, ErrStorageUnconfigured
	}
	offset = max(offset, 0)
	if limit <= 0 {
		limit = DefaultLogLimit
	}
	limit = min(limit, MaxLogLineLimit)
	st, err := s.logs.State(ctx, jobID)
	if err != nil {
		return nil, err
	}
	page := &model.WorkflowLogPage{Offset: offset, NextOffset: offset, TotalLines: st.Lines, Truncated: st.Truncated, Lines: []string{}}
	if offset >= st.Lines {
		return page, nil
	}
	chunks, err := s.logs.ChunksBetween(ctx, jobID, offset, offset+limit)
	if err != nil {
		return nil, err
	}
	for _, c := range chunks {
		if c.FirstLine > page.NextOffset {
			break
		}
		lines, err := s.chunkLines(ctx, c)
		if errors.Is(err, storage.ErrNotFound) {
			break
		}
		if err != nil {
			return nil, err
		}
		for i := max(page.NextOffset-c.FirstLine, 0); i < len(lines) && len(page.Lines) < limit; i++ {
			page.Lines = append(page.Lines, lines[i])
			page.NextOffset++
		}
	}
	return page, nil
}

func (s *WorkflowLogService) chunkLines(ctx context.Context, c model.WorkflowLogChunk) ([]string, error) {
	body, err := s.backend.Get(ctx, c.StorageKey)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	b, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n"), nil
}

// WriteLog copies the whole log of the Job to w, chunk after chunk. It stops at the first chunk whose object is
// gone, so a download is never a log with a hole in it.
func (s *WorkflowLogService) WriteLog(ctx context.Context, jobID int64, w io.Writer) error {
	if s.backend == nil {
		return ErrStorageUnconfigured
	}
	chunks, err := s.logs.ChunksBetween(ctx, jobID, 0, 1<<31-1)
	if err != nil {
		return err
	}
	next := 0
	for _, c := range chunks {
		if c.FirstLine != next {
			return nil
		}
		body, err := s.backend.Get(ctx, c.StorageKey)
		if errors.Is(err, storage.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		_, err = io.Copy(w, body)
		_ = body.Close()
		if err != nil {
			return err
		}
		next += c.LineCount
	}
	return nil
}

// UploadArtifact spools r to a temp file while hashing it, because S3 needs the length before the first byte, then
// writes the object before the row so a row never names a missing object. r is the archive the runner built.
func (s *WorkflowLogService) UploadArtifact(ctx context.Context, tok *JobToken, name string, r io.Reader) (*model.WorkflowArtifact, error) {
	if s.backend == nil {
		return nil, ErrStorageUnconfigured
	}
	name, err := CleanReleaseAssetName(name)
	if err != nil {
		return nil, ErrArtifactName
	}
	if n, err := s.logs.ArtifactCount(ctx, tok.RunID, tok.JobID); err != nil {
		return nil, err
	} else if n >= s.cfg.MaxArtifactsPerRun {
		return nil, s.countError()
	}
	tmp, err := os.CreateTemp("", "cz-artifact-*")
	if err != nil {
		return nil, fmt.Errorf("artifact spool: %w", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, sum), io.LimitReader(r, s.cfg.MaxArtifactBytes+1))
	if err != nil {
		return nil, fmt.Errorf("artifact spool: %w", err)
	}
	if n > s.cfg.MaxArtifactBytes {
		return nil, &LimitError{Err: ErrArtifactTooLarge,
			Msg: fmt.Sprintf("artifact %q is larger than the %s limit (ci.max_artifact_bytes)", name, humanBytes(s.cfg.MaxArtifactBytes))}
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("artifact spool: %w", err)
	}
	a := &model.WorkflowArtifact{
		RunID: tok.RunID, JobID: tok.JobID, RepoID: tok.RepoID, Name: name, Size: n,
		SHA256:     hex.EncodeToString(sum.Sum(nil)),
		StorageKey: fmt.Sprintf("ci/artifacts/repo/%d/run/%d/%s.zip", tok.RepoID, tok.RunID, attachment.NewToken()),
		ExpiresAt:  time.Now().Add(s.cfg.ArtifactRetention),
	}
	if err := s.backend.Put(ctx, a.StorageKey, tmp, n); err != nil {
		return nil, fmt.Errorf("store artifact: %w", err)
	}
	if err := s.logs.CreateArtifact(ctx, a, s.cfg.MaxArtifactsPerRun); err != nil {
		s.deleteObject(ctx, a.StorageKey)
		switch {
		case errors.Is(err, store.ErrArtifactLimit):
			return nil, s.countError()
		case errors.Is(err, store.ErrArtifactNameTaken):
			return nil, fmt.Errorf("%w: %q", ErrArtifactNameTaken, name)
		}
		return nil, err
	}
	return a, nil
}

func (s *WorkflowLogService) countError() error {
	return &LimitError{Err: ErrArtifactLimit,
		Msg: fmt.Sprintf("a Run can have at most %d artifacts (ci.max_artifacts_per_run)", s.cfg.MaxArtifactsPerRun)}
}

// GetArtifact returns ErrArtifactNotFound for a missing row and for one past its expiry, whose object the sweep has
// yet to delete.
func (s *WorkflowLogService) GetArtifact(ctx context.Context, id int64) (*model.WorkflowArtifact, error) {
	a, err := s.logs.GetArtifact(ctx, id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !a.ExpiresAt.After(time.Now())) {
		return nil, ErrArtifactNotFound
	}
	return a, err
}

// ListArtifacts returns the unexpired artifacts of a Run.
func (s *WorkflowLogService) ListArtifacts(ctx context.Context, runID int64) ([]model.WorkflowArtifact, error) {
	all, err := s.logs.ListArtifacts(ctx, runID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := all[:0]
	for _, a := range all {
		if a.ExpiresAt.After(now) {
			out = append(out, a)
		}
	}
	return out, nil
}

// OpenArtifact returns storage.ErrNotFound when the row outlived its object.
func (s *WorkflowLogService) OpenArtifact(ctx context.Context, a *model.WorkflowArtifact) (io.ReadCloser, error) {
	if s.backend == nil {
		return nil, ErrStorageUnconfigured
	}
	return s.backend.Get(ctx, a.StorageKey)
}

// Sweep deletes the chunks and artifacts that have expired and the ones whose Job or repo is gone, and returns how
// many rows it removed. The rows go first and the objects after, so a failed object delete leaves an orphan, which is
// safe to delete, rather than a row naming a missing object. Run rows and statuses are untouched.
func (s *WorkflowLogService) Sweep(ctx context.Context, now time.Time) (int, error) {
	total := 0
	for _, del := range []func() ([]string, error){
		func() ([]string, error) {
			return s.logs.DeleteExpiredChunks(ctx, now.Add(-s.cfg.LogRetention), sweepBatch)
		},
		func() ([]string, error) { return s.logs.DeleteExpiredArtifacts(ctx, now, sweepBatch) },
		func() ([]string, error) { return s.logs.DeleteOrphanChunks(ctx, sweepBatch) },
		func() ([]string, error) { return s.logs.DeleteOrphanArtifacts(ctx, sweepBatch) },
	} {
		for {
			keys, err := del()
			if err != nil {
				return total, err
			}
			s.deleteObjects(ctx, keys)
			total += len(keys)
			if len(keys) < sweepBatch {
				break
			}
		}
	}
	return total, nil
}

// DeleteForRepo removes a purged repo's logs and artifacts.
func (s *WorkflowLogService) DeleteForRepo(ctx context.Context, repoID int64) error {
	keys, err := s.logs.DeleteForRepo(ctx, repoID)
	s.deleteObjects(ctx, keys)
	return err
}

// SweepOrphans removes the logs and artifacts of repos and Jobs that no longer exist, for the delete paths that
// cascade whole repos away (a user or an org).
func (s *WorkflowLogService) SweepOrphans(ctx context.Context) {
	for _, del := range []func() ([]string, error){
		func() ([]string, error) { return s.logs.DeleteOrphanChunks(ctx, sweepBatch) },
		func() ([]string, error) { return s.logs.DeleteOrphanArtifacts(ctx, sweepBatch) },
	} {
		for {
			keys, err := del()
			if err != nil {
				slog.Warn("ci logs: orphan sweep failed; the hourly sweep will retry", "error", err)
				break
			}
			s.deleteObjects(ctx, keys)
			if len(keys) < sweepBatch {
				break
			}
		}
	}
}

// Run sweeps every hour until ctx ends.
func (s *WorkflowLogService) Run(ctx context.Context) {
	t := time.NewTicker(logSweepEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := s.Sweep(ctx, time.Now()); err != nil {
				slog.Error("ci log and artifact sweep failed", "error", err)
			} else if n > 0 {
				slog.Info("ci log and artifact sweep completed", "deleted", n)
			}
		}
	}
}

func (s *WorkflowLogService) deleteObjects(ctx context.Context, keys []string) {
	if s.backend == nil {
		return
	}
	for _, k := range keys {
		s.deleteObject(ctx, k)
	}
}

func (s *WorkflowLogService) deleteObject(ctx context.Context, key string) {
	if err := s.backend.Delete(context.WithoutCancel(ctx), key); err != nil {
		slog.Warn("ci logs: delete object failed; leaving an orphan", "key", key, "error", err)
	}
}
