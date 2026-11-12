package service

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

const (
	// ExportRetention is how long a finished or failed export stays before the sweep deletes it.
	ExportRetention = 7 * 24 * time.Hour
	// ExportMinGap is the least time between two requests from one user.
	ExportMinGap = 24 * time.Hour

	// exportStale outlives any plausible build, so only an export whose worker died is claimed twice.
	exportStale        = time.Hour
	exportPollInterval = 30 * time.Second
	exportSweepBatch   = 100

	exportFailedMessage = "The export failed. Request a new one."
)

var (
	ErrExportInProgress = store.ErrExportActive
	ErrExportTooSoon    = store.ErrExportTooSoon
	ErrExportNotFound   = errors.New("export not found")
)

// ExportService builds a zip of a user's own content in the background and keeps it for ExportRetention. It does
// not confirm the user: callers re-authenticate before Open.
type ExportService struct {
	exports *store.ExportStore
	users   *store.UserStore
	email   *EmailService
	baseURL string
	backend storage.Backend
	wake    chan struct{}
}

func NewExportService(exports *store.ExportStore, users *store.UserStore, email *EmailService, baseURL string) *ExportService {
	return &ExportService{exports: exports, users: users, email: email, baseURL: baseURL, wake: make(chan struct{}, 1)}
}

// WithBackend must be called before the service handles a request.
func (s *ExportService) WithBackend(b storage.Backend) *ExportService {
	s.backend = b
	return s
}

// exportKey is derived from the row, so a key is known for an export that is still running.
func exportKey(e model.AccountExport) string {
	return fmt.Sprintf("exports/%d/%d.zip", e.UserID, e.ID)
}

// Request queues an export for userID and drops their earlier ones, so the page has one to show.
func (s *ExportService) Request(ctx context.Context, userID int64) (*model.AccountExport, error) {
	if s.backend == nil {
		return nil, ErrStorageUnconfigured
	}
	e, err := s.exports.Create(ctx, userID, ExportMinGap)
	if err != nil {
		return nil, err
	}
	older, err := s.exports.ListByUser(ctx, userID)
	if err != nil {
		slog.Warn("export: list earlier exports", "user_id", userID, "error", err)
	}
	for _, o := range older {
		if o.ID != e.ID {
			s.remove(ctx, o)
		}
	}
	s.Wake()
	return e, nil
}

// Wake makes Run look for queued exports now rather than at its next poll.
func (s *ExportService) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Latest is nil when userID has never requested an export.
func (s *ExportService) Latest(ctx context.Context, userID int64) (*model.AccountExport, error) {
	e, err := s.exports.Latest(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return e, err
}

// Open returns the zip of userID's export id. An export that is another user's, not ready, past its expiry or
// missing its object is the same ErrExportNotFound.
func (s *ExportService) Open(ctx context.Context, userID, id int64) (io.ReadCloser, error) {
	if s.backend == nil {
		return nil, ErrStorageUnconfigured
	}
	e, err := s.exports.Get(ctx, userID, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrExportNotFound
	}
	if err != nil {
		return nil, err
	}
	if e.Status != model.ExportReady || e.ExpiresAt == nil || !e.ExpiresAt.After(time.Now()) {
		return nil, ErrExportNotFound
	}
	rc, err := s.backend.Get(ctx, e.StorageKey)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, ErrExportNotFound
	}
	return rc, err
}

// ObjectKeys are the objects userID's exports hold or will hold, for DeleteUser to collect before the rows cascade away.
func (s *ExportService) ObjectKeys(ctx context.Context, userID int64) ([]string, error) {
	list, err := s.exports.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(list))
	for _, e := range list {
		keys = append(keys, exportKey(e))
	}
	return keys, nil
}

// DeleteObjects removes objects whose rows are already gone; a failure leaves an orphan under exports/ that is safe to delete.
func (s *ExportService) DeleteObjects(ctx context.Context, keys []string) {
	if s.backend == nil {
		return
	}
	for _, k := range keys {
		if err := s.backend.Delete(context.WithoutCancel(ctx), k); err != nil {
			slog.Warn("export: delete object failed; leaving an orphan", "key", k, "error", err)
		}
	}
}

// Run builds queued exports and sweeps expired ones until ctx ends.
func (s *ExportService) Run(ctx context.Context) {
	ticker := time.NewTicker(exportPollInterval)
	defer ticker.Stop()
	for {
		s.buildQueued(ctx)
		s.sweep(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.wake:
		}
	}
}

func (s *ExportService) buildQueued(ctx context.Context) {
	if s.backend == nil {
		return
	}
	for ctx.Err() == nil {
		e, err := s.exports.ClaimNext(ctx, exportStale)
		if errors.Is(err, sql.ErrNoRows) {
			return
		}
		if err != nil {
			slog.Error("export: claim failed", "error", err)
			return
		}
		s.finish(ctx, e)
	}
}

func (s *ExportService) finish(ctx context.Context, e *model.AccountExport) {
	err := s.safeBuild(ctx, e)
	if ctx.Err() != nil {
		// Shutting down: the lease runs out and another instance builds it again.
		return
	}
	if err != nil {
		slog.Error("export: build failed", "export_id", e.ID, "user_id", e.UserID, "error", err)
		if err := s.exports.MarkFailed(ctx, e.ID, exportFailedMessage, ExportRetention); err != nil {
			slog.Error("export: record failure", "export_id", e.ID, "error", err)
		}
		return
	}
	s.notifyReady(ctx, e.UserID)
}

func (s *ExportService) safeBuild(ctx context.Context, e *model.AccountExport) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return s.build(ctx, e)
}

// build spools the zip to a temp file because S3 needs the length before the first byte; memory stays flat since
// each file is streamed from its query.
func (s *ExportService) build(ctx context.Context, e *model.AccountExport) error {
	key := exportKey(*e)
	if !storage.ValidKey(key) {
		return fmt.Errorf("invalid export key %q", key)
	}
	tmp, err := os.CreateTemp("", "cz-account-export-*")
	if err != nil {
		return fmt.Errorf("spool: %w", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	if err := s.writeZip(ctx, tmp, e.UserID); err != nil {
		return err
	}
	size, err := tmp.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("spool size: %w", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("spool rewind: %w", err)
	}
	if err := s.backend.Put(ctx, key, tmp, size); err != nil {
		return fmt.Errorf("store export: %w", err)
	}
	ok, err := s.exports.MarkReady(ctx, e.ID, key, ExportRetention)
	if err != nil || !ok {
		// The row is gone when the account was deleted mid-build; either way nothing points at the object.
		s.DeleteObjects(ctx, []string{key})
	}
	return err
}

func (s *ExportService) writeZip(ctx context.Context, dst io.Writer, userID int64) error {
	zw := zip.NewWriter(dst)
	if err := s.writeProfile(ctx, zw, userID); err != nil {
		return err
	}
	st := s.exports
	for _, f := range []func() error{
		func() error {
			return writeArray(zw, "ssh_keys.json", func(fn func(store.ExportSSHKey) error) error { return st.EachSSHKey(ctx, userID, fn) })
		},
		func() error {
			return writeArray(zw, "issues.json", func(fn func(store.ExportIssue) error) error { return st.EachIssue(ctx, userID, fn) })
		},
		func() error {
			return writeArray(zw, "comments.json", func(fn func(store.ExportComment) error) error { return st.EachComment(ctx, userID, fn) })
		},
		func() error {
			return writeArray(zw, "pull_requests.json", func(fn func(store.ExportPull) error) error { return st.EachPull(ctx, userID, fn) })
		},
		func() error {
			return writeArray(zw, "pull_reviews.json", func(fn func(store.ExportReview) error) error { return st.EachReview(ctx, userID, fn) })
		},
		func() error {
			return writeArray(zw, "pull_line_comments.json", func(fn func(store.ExportLineComment) error) error { return st.EachLineComment(ctx, userID, fn) })
		},
		func() error {
			return writeArray(zw, "gists.json", func(fn func(store.ExportGist) error) error { return st.EachGist(ctx, userID, fn) })
		},
		func() error {
			return writeArray(zw, "stars.json", func(fn func(store.ExportStar) error) error { return st.EachStar(ctx, userID, fn) })
		},
		func() error {
			return writeArray(zw, "watches.json", func(fn func(store.ExportWatch) error) error { return st.EachWatch(ctx, userID, fn) })
		},
	} {
		if err := f(); err != nil {
			return err
		}
	}
	return zw.Close()
}

func (s *ExportService) writeProfile(ctx context.Context, zw *zip.Writer, userID int64) error {
	p, emails, err := s.exports.Profile(ctx, userID)
	if err != nil {
		return err
	}
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "profile.json", Method: zip.Deflate, Modified: time.Now()})
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		store.ExportProfile
		AdditionalEmails []store.ExportEmail `json:"additional_emails"`
	}{p, emails})
}

// writeArray writes one JSON array into the zip entry name, one element per line, as each row arrives.
func writeArray[T any](zw *zip.Writer, name string, each func(func(T) error) error) error {
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Now()})
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, "[\n"); err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	n := 0
	err = each(func(v T) error {
		if n > 0 {
			if _, err := io.WriteString(w, ","); err != nil {
				return err
			}
		}
		n++
		return enc.Encode(v)
	})
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, "]\n")
	return err
}

func (s *ExportService) notifyReady(ctx context.Context, userID int64) {
	if s.email == nil || !s.email.Enabled() {
		return
	}
	u, err := s.users.GetByID(ctx, userID)
	if err != nil || u.Email == "" {
		return
	}
	body := fmt.Sprintf("<p>Your Cloudzilla data export is ready. <a href=\"%s/settings#export\">Download it from your account settings</a> within %d days; you'll be asked to confirm it's you.</p>",
		s.baseURL, int(ExportRetention.Hours()/24))
	if err := s.email.Send(u.Email, "Your Cloudzilla data export is ready", body); err != nil {
		slog.Warn("export: send ready email", "user_id", userID, "error", err)
	}
}

// sweep deletes the object before the row: a row deleted first would strand its object.
func (s *ExportService) sweep(ctx context.Context) {
	if s.backend == nil {
		return
	}
	expired, err := s.exports.ListExpired(ctx, exportSweepBatch)
	if err != nil {
		slog.Error("export: list expired", "error", err)
		return
	}
	for _, e := range expired {
		s.remove(ctx, e)
	}
}

func (s *ExportService) remove(ctx context.Context, e model.AccountExport) {
	if s.backend != nil {
		if err := s.backend.Delete(context.WithoutCancel(ctx), exportKey(e)); err != nil {
			slog.Warn("export: delete object failed; will retry", "export_id", e.ID, "error", err)
			return
		}
	}
	if err := s.exports.Delete(ctx, e.ID); err != nil {
		slog.Warn("export: delete row failed", "export_id", e.ID, "error", err)
	}
}
