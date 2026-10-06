package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gogit "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/secretbox"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

const mirrorCredentialPurpose = "mirror-credential"

// mirrorSideEffectTimeout bounds a sync's post-fetch work, as for a push.
const mirrorSideEffectTimeout = 5 * time.Minute

// Not a mirror's +refs/*:refs/*, which would also copy GitHub's refs/pull/*.
var mirrorRefSpecs = []gitconfig.RefSpec{"+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*"}

var ErrMirrorNoSecretKey = errors.New("mirroring with credentials needs security.secret_key")

// MirrorSyncError is a failed sync, worded for the repo's admins.
type MirrorSyncError struct {
	Msg string
	Err error
}

func (e *MirrorSyncError) Error() string { return e.Msg }
func (e *MirrorSyncError) Unwrap() error { return e.Err }

// MirrorService keeps pull mirrors in step with their upstreams.
type MirrorService struct {
	mirrors   *store.MirrorStore
	repoStore *store.RepoStore
	repos     *RepoService
	webhooks  *WebhookService
	index     *IndexService
	deps      *DependencyService
	secrets   *secretbox.Box
	git       config.GitConfig
	cfg       config.MirrorConfig
	wake      chan struct{}

	// Shutdown cancels stopCtx, under which syncs run, and waits on inflight.
	stopCtx  context.Context
	stop     context.CancelFunc
	stopping atomic.Bool
	inflight sync.WaitGroup
}

func NewMirrorService(mirrors *store.MirrorStore, repoStore *store.RepoStore, repos *RepoService, webhooks *WebhookService,
	index *IndexService, deps *DependencyService, secrets *secretbox.Box, git config.GitConfig, cfg config.MirrorConfig) *MirrorService {
	stopCtx, stop := context.WithCancel(context.Background())
	return &MirrorService{mirrors: mirrors, repoStore: repoStore, repos: repos, webhooks: webhooks, index: index, deps: deps,
		secrets: secrets, git: git, cfg: cfg, wake: make(chan struct{}, 1), stopCtx: stopCtx, stop: stop}
}

// Get wraps sql.ErrNoRows when the repo isn't a mirror.
func (s *MirrorService) Get(ctx context.Context, repoID int64) (*model.RepoMirror, error) {
	return s.mirrors.Get(ctx, repoID)
}

// SealToken encrypts an upstream token for storage.
func (s *MirrorService) SealToken(token string) ([]byte, error) {
	sealed, err := s.secrets.Seal(mirrorCredentialPurpose, []byte(token))
	if errors.Is(err, secretbox.ErrNoKey) {
		return nil, ErrMirrorNoSecretKey
	}
	return sealed, err
}

// Sync fetches the upstream's branches and tags into the mirror, pruning
// what the upstream dropped, follows its default branch, and runs the
// side effects of a push for the refs that moved. Failures the admins can
// act on are a *MirrorSyncError.
func (s *MirrorService) Sync(ctx context.Context, m *model.RepoMirror) error {
	installImportTransport()
	repo, err := s.repoStore.GetByID(ctx, m.RepoID)
	if err != nil {
		return fmt.Errorf("mirror sync: load repo %d: %w", m.RepoID, err)
	}
	auth, err := s.auth(m)
	if err != nil {
		return err
	}
	gitDir, err := RepoDir(s.git.ReposRoot, repo.OwnerName, repo.Name+".git")
	if err != nil {
		return err
	}
	local, err := gogit.PlainOpen(gitDir)
	if err != nil {
		return fmt.Errorf("mirror sync: open repo: %w", err)
	}

	guard := &importGuard{
		allowLocal:    s.cfg.AllowLocalNetworks,
		maxPackBytes:  s.git.MaxPackBytes,
		maxRefsBytes:  importMaxRefsBytes,
		maxErrorBytes: importMaxErrorBodyBytes,
	}
	ctx = withImportGuard(ctx, guard)
	before, err := mirroredRefs(local)
	if err != nil {
		return err
	}
	remote := gogit.NewRemote(local.Storer, &gitconfig.RemoteConfig{
		Name: "upstream", URLs: []string{m.RemoteURL}, Fetch: mirrorRefSpecs,
	})
	advertised, err := remote.ListContext(ctx, &gogit.ListOptions{Auth: auth})
	if err != nil {
		return s.syncError(ctx, repo, guard, err)
	}
	// With prune, a fetch from an upstream with no branches would delete every one here.
	branch, err := defaultImportBranch(advertised)
	if err != nil {
		return &MirrorSyncError{Msg: "The source repository has no branches. The mirror keeps its last copy.", Err: err}
	}
	err = remote.FetchContext(ctx, &gogit.FetchOptions{Auth: auth, Tags: gogit.NoTags, Prune: true, Force: true})
	if err != nil && !errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return s.syncError(ctx, repo, guard, err)
	}
	after, err := mirroredRefs(local)
	if err != nil {
		return err
	}

	// The refs have moved: their side effects run even if what follows fails
	// or the sync's own time is nearly up, since the next sync won't see them.
	sideCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), mirrorSideEffectTimeout)
	defer cancel()
	headErr := s.followHead(sideCtx, repo, local, branch)
	s.afterSync(sideCtx, repo, local, refChanges(before, after))
	return headErr
}

func (s *MirrorService) auth(m *model.RepoMirror) (transport.AuthMethod, error) {
	if m.AuthTokenEnc == nil {
		return nil, nil
	}
	token, err := s.secrets.Open(mirrorCredentialPurpose, m.AuthTokenEnc)
	switch {
	case errors.Is(err, secretbox.ErrNoKey):
		return nil, &MirrorSyncError{Msg: "The stored token can't be read because security.secret_key is not set.", Err: err}
	case err != nil:
		return nil, &MirrorSyncError{Msg: "The stored token can't be decrypted; re-enter the token in the mirror settings.", Err: err}
	}
	return importAuth(m.AuthUsername, string(token)), nil
}

func (s *MirrorService) syncError(ctx context.Context, repo *model.Repository, guard *importGuard, err error) error {
	var blocked *PrivateNetworkError
	var tooLarge *importSizeError
	stopped := guard.failure()
	msg := ""
	switch {
	case errors.As(stopped, &blocked):
		msg = blocked.Host + " resolves to a private network address. An administrator can allow this with mirror.allow_local_networks."
	case errors.As(stopped, &tooLarge):
		msg = tooLarge.message()
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		msg = "The sync took longer than " + formatImportTimeout(s.cfg.Timeout) + " and was stopped."
	case errors.Is(err, transport.ErrAuthenticationRequired), errors.Is(err, transport.ErrAuthorizationFailed),
		errors.Is(err, transport.ErrRepositoryNotFound):
		msg = "The source repository was not found, or the stored credentials were rejected."
	case errors.Is(err, transport.ErrEmptyRemoteRepository):
		msg = "The source repository is empty. The mirror keeps its last copy."
	default:
		slog.Error("mirror sync failed", "repo_id", repo.ID, "owner", repo.OwnerName, "repo", repo.Name, "error", err)
		msg = "The sync failed. Check that the source repository is reachable."
	}
	return &MirrorSyncError{Msg: msg, Err: err}
}

// followHead points HEAD and the default branch at branch, the upstream's
// default as an import would choose it.
func (s *MirrorService) followHead(ctx context.Context, repo *model.Repository, local *gogit.Repository, branch string) error {
	target := plumbing.NewBranchReferenceName(branch)
	if head, err := local.Storer.Reference(plumbing.HEAD); err != nil || head.Target() != target {
		if err := local.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, target)); err != nil {
			return fmt.Errorf("mirror sync: set HEAD: %w", err)
		}
	}
	if branch != repo.DefaultBranch {
		if err := s.repoStore.SetDefaultBranch(ctx, repo.ID, branch); err != nil {
			return err
		}
		repo.DefaultBranch = branch
	}
	return nil
}

// afterSync runs what a push of cmds would, minus activity events and
// notifications: a sync has no actor, and an upstream's commits are no one
// here's news.
func (s *MirrorService) afterSync(ctx context.Context, repo *model.Repository, local *gogit.Repository, cmds []*packp.Command) {
	if len(cmds) == 0 {
		return
	}
	for _, cmd := range cmds {
		if !cmd.Name.IsBranch() || cmd.Action() == packp.Delete {
			continue
		}
		s.webhooks.Dispatch(repo.ID, "push", s.webhooks.PushPayload(*repo, "", cmd.Name.Short(), cmd.New.String()))
	}
	if err := s.repos.OnPostReceive(ctx, repo, local, cmds); err != nil {
		slog.Error("mirror sync: post-receive failed", "repo_id", repo.ID, "error", err)
	}
	if err := s.index.IndexRepo(ctx, repo); err != nil {
		slog.Error("mirror sync: re-index failed", "repo_id", repo.ID, "error", err)
	}
	if err := s.deps.ParseAndStore(ctx, repo); err != nil {
		slog.Error("mirror sync: dependency parse failed", "repo_id", repo.ID, "error", err)
	}
}

// mirroredRefs maps the branches and tags a sync manages to their hashes.
func mirroredRefs(repo *gogit.Repository) (map[plumbing.ReferenceName]plumbing.Hash, error) {
	iter, err := repo.Storer.IterReferences()
	if err != nil {
		return nil, fmt.Errorf("mirror sync: list refs: %w", err)
	}
	refs := map[plumbing.ReferenceName]plumbing.Hash{}
	err = iter.ForEach(func(r *plumbing.Reference) error {
		if r.Type() == plumbing.HashReference && (r.Name().IsBranch() || r.Name().IsTag()) {
			refs[r.Name()] = r.Hash()
		}
		return nil
	})
	return refs, err
}

// refChanges is the push that would have turned before into after.
func refChanges(before, after map[plumbing.ReferenceName]plumbing.Hash) []*packp.Command {
	var cmds []*packp.Command
	for name, old := range before {
		if after[name] != old {
			cmds = append(cmds, &packp.Command{Name: name, Old: old, New: after[name]})
		}
	}
	for name, h := range after {
		if _, ok := before[name]; !ok {
			cmds = append(cmds, &packp.Command{Name: name, Old: plumbing.ZeroHash, New: h})
		}
	}
	sort.Slice(cmds, func(i, j int) bool { return strings.Compare(cmds[i].Name.String(), cmds[j].Name.String()) < 0 })
	return cmds
}

var (
	ErrMirrorsDisabled = errors.New("mirroring is turned off on this instance")
	ErrMirrorInterval  = errors.New("invalid sync interval")
)

// Enabled reports whether mirror.enabled is set.
func (s *MirrorService) Enabled() bool { return s.cfg.Enabled }

// Interval resolves a requested sync interval: zero means the default, and
// anything outside mirror.min_interval to MaxMirrorInterval is refused.
func (s *MirrorService) Interval(requested time.Duration) (time.Duration, error) {
	if requested == 0 {
		return s.cfg.DefaultInterval, nil
	}
	if requested < s.cfg.MinInterval || requested > config.MaxMirrorInterval {
		return 0, fmt.Errorf("%w: it must be between %s and %s", ErrMirrorInterval,
			FormatMirrorInterval(s.cfg.MinInterval), FormatMirrorInterval(config.MaxMirrorInterval))
	}
	return requested, nil
}

// MirrorIntervalChoice is one option of a form's sync-interval picker.
type MirrorIntervalChoice struct {
	Value   string
	Label   string
	Default bool
}

var mirrorIntervalPresets = []time.Duration{10 * time.Minute, time.Hour, 8 * time.Hour, 24 * time.Hour, 7 * 24 * time.Hour}

// IntervalChoices are the presets this instance allows, plus its default.
func (s *MirrorService) IntervalChoices() []MirrorIntervalChoice {
	durations := []time.Duration{s.cfg.DefaultInterval}
	for _, d := range mirrorIntervalPresets {
		if d >= s.cfg.MinInterval && d <= config.MaxMirrorInterval && d != s.cfg.DefaultInterval {
			durations = append(durations, d)
		}
	}
	slices.Sort(durations)
	choices := make([]MirrorIntervalChoice, len(durations))
	for i, d := range durations {
		choices[i] = MirrorIntervalChoice{Value: d.String(), Label: FormatMirrorInterval(d), Default: d == s.cfg.DefaultInterval}
	}
	return choices
}

// FormatMirrorInterval names d in its largest whole unit: "1 day", "8 hours";
// anything else falls back to Go's form, such as "1h30m0s".
func FormatMirrorInterval(d time.Duration) string {
	for _, u := range []struct {
		size time.Duration
		name string
	}{{7 * 24 * time.Hour, "week"}, {24 * time.Hour, "day"}, {time.Hour, "hour"}, {time.Minute, "minute"}} {
		if d >= u.size && d%u.size == 0 {
			n := int(d / u.size)
			if n == 1 {
				return "1 " + u.name
			}
			return strconv.Itoa(n) + " " + u.name + "s"
		}
	}
	return d.String()
}

// mirrorSpec is a mirror an import creates once its repo exists.
type mirrorSpec struct {
	remoteURL, username string
	tokenEnc            []byte
	interval            time.Duration
	createdBy           int64
}

func (s *MirrorService) create(ctx context.Context, repo *model.Repository, spec mirrorSpec) error {
	return s.mirrors.Create(ctx, &model.RepoMirror{
		RepoID: repo.ID, RemoteURL: spec.remoteURL, AuthUsername: spec.username, AuthTokenEnc: spec.tokenEnc,
		Interval: spec.interval, CreatedBy: spec.createdBy,
	})
}

// indexImported does for a new mirror what its later syncs will: plain
// imports skip it, but a mirror's search and dependencies must start current.
func (s *MirrorService) indexImported(ctx context.Context, repo *model.Repository) {
	if err := s.index.IndexRepo(ctx, repo); err != nil {
		slog.Error("mirror import: index failed", "repo_id", repo.ID, "error", err)
	}
	if err := s.deps.ParseAndStore(ctx, repo); err != nil {
		slog.Error("mirror import: dependency parse failed", "repo_id", repo.ID, "error", err)
	}
}

// MirrorUpdate changes a mirror's settings; a nil field keeps its value, and
// so does an empty AuthToken, since a stored token is never shown to edit.
type MirrorUpdate struct {
	RemoteURL    *string
	AuthUsername *string
	AuthToken    *string
	ClearToken   bool
	Interval     *time.Duration
}

// Update saves u and returns the fields it changed. A new source or new
// credentials are tried at once; a new interval counts from the last sync.
func (s *MirrorService) Update(ctx context.Context, repoID int64, u MirrorUpdate) (*model.RepoMirror, []string, error) {
	m, err := s.mirrors.Get(ctx, repoID)
	if err != nil {
		return nil, nil, err
	}
	var changed []string
	if u.RemoteURL != nil {
		src, err := ParseImportURL(*u.RemoteURL)
		if err != nil {
			return nil, nil, err
		}
		if src != m.RemoteURL {
			m.RemoteURL, changed = src, append(changed, "remote_url")
		}
	}
	if u.AuthUsername != nil {
		if name := strings.TrimSpace(*u.AuthUsername); name != m.AuthUsername {
			m.AuthUsername, changed = name, append(changed, "auth_username")
		}
	}
	switch {
	case u.ClearToken && m.AuthTokenEnc != nil:
		m.AuthTokenEnc, changed = nil, append(changed, "auth_token")
	case !u.ClearToken && u.AuthToken != nil && *u.AuthToken != "":
		if m.AuthTokenEnc, err = s.SealToken(*u.AuthToken); err != nil {
			return nil, nil, err
		}
		changed = append(changed, "auth_token")
	}
	if m.AuthTokenEnc != nil && m.AuthUsername == "" {
		return nil, nil, ErrImportCredentials
	}
	intervalChanged := false
	if u.Interval != nil {
		interval, err := s.Interval(*u.Interval)
		if err != nil {
			return nil, nil, err
		}
		if interval != m.Interval {
			m.Interval, intervalChanged, changed = interval, true, append(changed, "interval")
		}
	}

	sched := store.MirrorKeepSchedule
	switch {
	case slices.ContainsFunc(changed, func(f string) bool { return f != "interval" }):
		sched = store.MirrorSyncNow
	case intervalChanged:
		sched = store.MirrorFromLastSync
	}
	if err := s.mirrors.Update(ctx, m, sched); err != nil {
		return nil, nil, err
	}
	if sched == store.MirrorSyncNow {
		s.Wake()
	}
	return m, changed, nil
}

var ErrMirrorSyncRunning = errors.New("a sync is running")

// Stop turns a mirror into a regular repository, deleting its stored token.
// It refuses while a sync runs, which would force-fetch over pushes to the
// now writable repo.
func (s *MirrorService) Stop(ctx context.Context, repoID int64) error {
	deleted, err := s.mirrors.DeleteUnleased(ctx, repoID)
	if err != nil {
		return err
	}
	if !deleted {
		if _, err := s.mirrors.Get(ctx, repoID); err == nil {
			return ErrMirrorSyncRunning
		}
	}
	return nil
}
