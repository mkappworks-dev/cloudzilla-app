package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/mkappworks-dev/cloudzilla-app/internal/concurrency"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

const (
	importConcurrency  = 3
	importPerUserLimit = 5
	importJobRetention = time.Hour
	// Refusals finish in milliseconds; without this a loop of them grows jobs for an hour.
	importFinishedPerUser = 20
	// Owner names start with a letter or digit, so this can't collide with an owner dir.
	importTmpDirName = ".import-tmp"
)

const importFailedMessage = "The import failed."

var (
	ErrTooManyImports = fmt.Errorf("you already have %d imports in progress; wait for one to finish", importPerUserLimit)
	ErrImportNotFound = errors.New("import not found")
)

// A var so a test can make the clone panic.
var cloneImport = cloneForImport

type ImportStatus string

const (
	ImportQueued  ImportStatus = "queued"
	ImportRunning ImportStatus = "running"
	ImportDone    ImportStatus = "done"
	ImportFailed  ImportStatus = "failed"
)

type ImportRequest struct {
	CloneURL     string
	AuthUsername string
	AuthToken    string
	Owner        string
	Name         string
	Description  string
	Private      bool
	// Mirror keeps the new repo in sync with the source, every MirrorInterval
	// (zero: mirror.default_interval), and stores the token sealed.
	Mirror         bool
	MirrorInterval time.Duration
}

// ImportJob is a snapshot of one import. It never holds credentials.
type ImportJob struct {
	ID         string
	UserID     int64
	SourceURL  string
	Owner      string
	Name       string
	Status     ImportStatus
	Progress   string
	Error      string
	FinishedAt time.Time
}

func (j ImportJob) Finished() bool {
	return j.Status == ImportDone || j.Status == ImportFailed
}

// Status, Error and FinishedAt change under ImportService.mu; the other
// fields are fixed when Start creates the job.
type importJob struct {
	ImportJob
	progress *importProgress
}

// ImportService runs imports in the background. Jobs live in memory: a
// restart drops imports in flight, and RemoveStaleTemp clears their clones.
type ImportService struct {
	repo         *RepoService
	root         string
	maxPackBytes int64
	cfg          config.ImportConfig
	slots        chan struct{}
	mirrors      *MirrorService

	mu   sync.Mutex
	jobs map[string]*importJob
}

func NewImportService(repo *RepoService, git config.GitConfig, cfg config.ImportConfig) *ImportService {
	installImportTransport()
	return &ImportService{
		repo:         repo,
		root:         git.ReposRoot,
		maxPackBytes: git.MaxPackBytes,
		cfg:          cfg,
		slots:        make(chan struct{}, importConcurrency),
		jobs:         map[string]*importJob{},
	}
}

func (s *ImportService) WithMirrors(m *MirrorService) *ImportService {
	s.mirrors = m
	return s
}

func (s *ImportService) Start(ctx context.Context, actorID int64, actorUsername string, req ImportRequest) (ImportJob, error) {
	src, err := ParseImportURL(req.CloneURL)
	if err != nil {
		return ImportJob{}, err
	}
	if (req.AuthUsername == "") != (req.AuthToken == "") {
		return ImportJob{}, ErrImportCredentials
	}
	mirror, err := s.mirrorSpec(src, actorID, req)
	if err != nil {
		return ImportJob{}, err
	}
	target, err := s.repo.ResolveRepoTarget(ctx, actorID, actorUsername, req.Owner)
	if err != nil {
		return ImportJob{}, err
	}
	if err := s.repo.CheckImportName(ctx, target.OwnerName, req.Name); err != nil {
		return ImportJob{}, err
	}
	id, err := newImportID()
	if err != nil {
		return ImportJob{}, err
	}
	job := &importJob{
		ImportJob: ImportJob{ID: id, UserID: actorID, SourceURL: src, Owner: target.OwnerName, Name: req.Name, Status: ImportQueued},
		progress:  &importProgress{},
	}

	s.mu.Lock()
	s.sweepLocked(time.Now())
	if s.activeLocked(actorID) >= importPerUserLimit {
		s.mu.Unlock()
		return ImportJob{}, ErrTooManyImports
	}
	s.jobs[id] = job
	s.evictFinishedLocked(actorID)
	snap := job.ImportJob
	s.mu.Unlock()

	auth := importAuth(req.AuthUsername, req.AuthToken)
	concurrency.Go("repo.import", func() { s.run(job, target, req.Description, req.Private, auth, mirror) })
	return snap, nil
}

// Get answers ErrImportNotFound for another user's job too, so a leaked ID reveals nothing.
func (s *ImportService) Get(userID int64, id string) (ImportJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(time.Now())
	job, ok := s.jobs[id]
	if !ok || job.UserID != userID {
		return ImportJob{}, ErrImportNotFound
	}
	snap := job.ImportJob
	snap.Progress = job.progress.String()
	return snap, nil
}

func (s *ImportService) RemoveStaleTemp() error {
	return os.RemoveAll(filepath.Join(s.root, importTmpDirName))
}

// mirrorSpec is nil for a plain import.
func (s *ImportService) mirrorSpec(src string, actorID int64, req ImportRequest) (*mirrorSpec, error) {
	if !req.Mirror {
		return nil, nil
	}
	if s.mirrors == nil || !s.mirrors.Enabled() {
		return nil, ErrMirrorsDisabled
	}
	interval, err := s.mirrors.Interval(req.MirrorInterval)
	if err != nil {
		return nil, err
	}
	spec := &mirrorSpec{remoteURL: src, username: req.AuthUsername, interval: interval, createdBy: actorID}
	if req.AuthToken != "" {
		if spec.tokenEnc, err = s.mirrors.SealToken(req.AuthToken); err != nil {
			return nil, err
		}
	}
	return spec, nil
}

func (s *ImportService) run(job *importJob, target RepoTarget, description string, private bool, auth transport.AuthMethod, mirror *mirrorSpec) {
	s.slots <- struct{}{}
	defer func() { <-s.slots }()
	s.setStatus(job, ImportRunning)

	dir := filepath.Join(s.root, importTmpDirName, job.ID)
	failure := s.attempt(dir, job, target, description, private, auth, mirror)
	_ = os.RemoveAll(dir)
	s.finish(job, failure)
}

// attempt recovers a panic itself: concurrency.Go's recover would leave the
// job running, holding one of the user's slots and polled forever.
func (s *ImportService) attempt(dir string, job *importJob, target RepoTarget, description string, private bool, auth transport.AuthMethod, mirror *mirrorSpec) (failure string) {
	defer func() {
		if p := recover(); p != nil {
			slog.Error("repo import panicked", "job_id", job.ID, "owner", job.Owner, "name", job.Name,
				"source_host", importHost(job.SourceURL), "panic", p, "stack", string(debug.Stack()))
			failure = importFailedMessage
		}
	}()
	guard := &importGuard{
		allowLocal:    s.cfg.AllowLocalNetworks,
		maxPackBytes:  s.maxPackBytes,
		maxRefsBytes:  importMaxRefsBytes,
		maxErrorBytes: importMaxErrorBodyBytes,
	}
	ctx, cancel := s.jobContext(withImportGuard(context.Background(), guard))
	defer cancel()
	if err := s.cloneAndPublish(ctx, dir, job, target, description, private, auth, mirror); err != nil {
		return s.failureMessage(ctx, job, guard, err)
	}
	return ""
}

func (s *ImportService) jobContext(parent context.Context) (context.Context, context.CancelFunc) {
	if s.cfg.Timeout > 0 {
		return context.WithTimeout(parent, s.cfg.Timeout)
	}
	return context.WithCancel(parent)
}

func (s *ImportService) cloneAndPublish(ctx context.Context, dir string, job *importJob, target RepoTarget, description string, private bool, auth transport.AuthMethod, mirror *mirrorSpec) error {
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return fmt.Errorf("create import temp dir: %w", err)
	}
	branch, err := cloneImport(ctx, dir, job.SourceURL, auth, job.progress)
	if err != nil {
		return err
	}
	if mirror == nil {
		_, err = s.repo.CreateFromImport(ctx, target, job.Name, description, private, branch, dir)
		return err
	}
	repo, err := s.repo.createFromImport(ctx, target, job.Name, description, private, branch, dir,
		func(ctx context.Context, r *model.Repository) error { return s.mirrors.create(ctx, r, *mirror) })
	if err != nil {
		return err
	}
	s.mirrors.indexImported(ctx, repo)
	return nil
}

func (s *ImportService) failureMessage(ctx context.Context, job *importJob, guard *importGuard, err error) string {
	var blocked *PrivateNetworkError
	var tooLarge *importSizeError
	stopped := guard.failure()
	switch {
	case errors.As(stopped, &blocked):
		return blocked.Host + " resolves to a private network address. An administrator can allow this with import.allow_local_networks."
	case errors.As(stopped, &tooLarge):
		return tooLarge.message()
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return "The import took longer than " + formatImportTimeout(s.cfg.Timeout) + " and was stopped."
	case errors.Is(err, transport.ErrAuthenticationRequired), errors.Is(err, transport.ErrAuthorizationFailed),
		errors.Is(err, transport.ErrRepositoryNotFound):
		return "Repository not found, or it needs a username and token."
	case errors.Is(err, ErrImportEmptySource):
		return "The source repository is empty — create a new repository instead."
	case errors.Is(err, ErrRepoNameTaken):
		return job.Owner + "/" + job.Name + " was created while the import ran."
	case errors.Is(err, ErrForbidden):
		return "You can no longer create repositories under " + job.Owner + "."
	}
	slog.Error("repo import failed", "job_id", job.ID, "owner", job.Owner, "name", job.Name,
		"source_host", importHost(job.SourceURL), "error", err)
	return importFailedMessage
}

func (s *ImportService) setStatus(job *importJob, status ImportStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job.Status = status
}

func (s *ImportService) finish(job *importJob, failure string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job.Status, job.FinishedAt = ImportDone, time.Now()
	if failure != "" {
		job.Status, job.Error = ImportFailed, failure
	}
}

func (s *ImportService) sweepLocked(now time.Time) {
	for id, job := range s.jobs {
		if job.Finished() && now.Sub(job.FinishedAt) > importJobRetention {
			delete(s.jobs, id)
		}
	}
}

func (s *ImportService) evictFinishedLocked(userID int64) {
	var finished []*importJob
	for _, job := range s.jobs {
		if job.UserID == userID && job.Finished() {
			finished = append(finished, job)
		}
	}
	if len(finished) <= importFinishedPerUser {
		return
	}
	slices.SortFunc(finished, func(a, b *importJob) int { return a.FinishedAt.Compare(b.FinishedAt) })
	for _, job := range finished[:len(finished)-importFinishedPerUser] {
		delete(s.jobs, job.ID)
	}
}

func (s *ImportService) activeLocked(userID int64) int {
	n := 0
	for _, job := range s.jobs {
		if job.UserID == userID && !job.Finished() {
			n++
		}
	}
	return n
}

func newImportID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func importHost(src string) string {
	u, err := url.Parse(src)
	if err != nil {
		return ""
	}
	return u.Host
}

func formatImportBytes(n int64) string {
	const gib, mib = 1 << 30, 1 << 20
	if n >= gib && n%gib == 0 {
		return fmt.Sprintf("%d GiB", n/gib)
	}
	return fmt.Sprintf("%d MiB", (n+mib-1)/mib)
}

// formatImportTimeout drops Duration.String's zero units: 30m, not 30m0s.
func formatImportTimeout(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = s[:len(s)-2]
	}
	if strings.HasSuffix(s, "h0m") {
		s = s[:len(s)-2]
	}
	return s
}
