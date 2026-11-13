package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/secretscan"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

const (
	backfillDetail = "history scan"
	// Matches the push path's per-blob cap (secret_scanning.max_blob_bytes).
	backfillMaxBlobBytes = 1 << 20
)

// SecretTokenResolver decides what a cloudzilla_token finding means. Without one, such findings are dropped as not
// live, since a czp_ lookalike that matches no stored hash is not a secret.
type SecretTokenResolver interface {
	// ResolveToken reports whether value is a live token of the instance. Revoking it is the implementation's business.
	ResolveToken(ctx context.Context, repo model.Repository, f secretscan.Finding) (live bool, err error)
}

// SecretScanNotifier is the part of NotificationService a scan reports through.
type SecretScanNotifier interface {
	NotifySecretAlert(ctx context.Context, repo model.Repository, alert *model.SecretScanAlert, count int)
}

// SecretScanService scans a repository's whole reachable history once, in the background, and records what it finds.
type SecretScanService struct {
	scans    *store.SecretScanStore
	repos    *store.RepoStore
	alerts   *SecretAlertService
	notifier SecretScanNotifier
	tokens   SecretTokenResolver
	git      config.GitConfig
	cfg      config.SecretScanningConfig
	wake     chan struct{}
}

func NewSecretScanService(scans *store.SecretScanStore, repos *store.RepoStore, alerts *SecretAlertService, notifier SecretScanNotifier,
	git config.GitConfig, cfg config.SecretScanningConfig) *SecretScanService {
	return &SecretScanService{scans: scans, repos: repos, alerts: alerts, notifier: notifier, git: git, cfg: cfg, wake: make(chan struct{}, 1)}
}

// WithTokenResolver plugs in the check for cloudzilla_token findings.
func (s *SecretScanService) WithTokenResolver(r SecretTokenResolver) *SecretScanService {
	s.tokens = r
	return s
}

// MarkPending queues a new scan of the repository and wakes the loop.
func (s *SecretScanService) MarkPending(ctx context.Context, repoID int64) error {
	if err := s.scans.MarkPending(ctx, repoID); err != nil {
		return err
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return nil
}

// ScanState returns the scan's state ('pending', 'running', 'done' or 'failed') and whether it stopped at its budget.
func (s *SecretScanService) ScanState(ctx context.Context, repoID int64) (string, bool, error) {
	return s.scans.State(ctx, repoID)
}

// ScanRepo scans one repository now and returns the alerts it opened. It does not touch the scan state: RunBackfill does.
func (s *SecretScanService) ScanRepo(ctx context.Context, repo *model.Repository) (opened []*model.SecretScanAlert, incomplete bool, err error) {
	dir, err := RepoDir(s.git.ReposRoot, repo.OwnerName, repo.Name+".git")
	if err != nil {
		return nil, false, err
	}
	local, err := gogit.PlainOpen(dir)
	if err != nil {
		return nil, false, fmt.Errorf("secret scan: open repo: %w", err)
	}
	w, err := s.scanHistory(ctx, repo, local.Storer)
	return w.opened, w.incomplete, err
}

func (s *SecretScanService) scanHistory(ctx context.Context, repo *model.Repository, st storage.Storer) (*historyWalk, error) {
	w := &historyWalk{
		svc: s, repo: repo, st: st,
		commits: map[plumbing.Hash]struct{}{}, trees: map[plumbing.Hash]struct{}{}, blobs: map[plumbing.Hash]struct{}{},
	}
	err := w.run(ctx)
	if errors.Is(err, errScanBudget) {
		w.incomplete = true
		err = nil
	}
	return w, err
}

var errScanBudget = errors.New("secret scan budget reached")

type historyWalk struct {
	svc        *SecretScanService
	repo       *model.Repository
	st         storage.Storer
	commits    map[plumbing.Hash]struct{}
	trees      map[plumbing.Hash]struct{}
	blobs      map[plumbing.Hash]struct{}
	opened     []*model.SecretScanAlert
	incomplete bool
	ref        string
	sha        string
}

func (w *historyWalk) run(ctx context.Context) error {
	iter, err := w.st.IterReferences()
	if err != nil {
		return fmt.Errorf("secret scan: list refs: %w", err)
	}
	tips := map[plumbing.ReferenceName]plumbing.Hash{}
	err = iter.ForEach(func(r *plumbing.Reference) error {
		if r.Type() == plumbing.HashReference && strings.HasPrefix(r.Name().String(), "refs/") {
			tips[r.Name()] = r.Hash()
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("secret scan: list refs: %w", err)
	}
	// A fixed order keeps the location chosen for a shared blob the same from run to run.
	names := make([]plumbing.ReferenceName, 0, len(tips))
	for n := range tips {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	for _, name := range names {
		if err := w.walkFrom(ctx, name.String(), tips[name]); err != nil {
			return err
		}
	}
	return nil
}

func (w *historyWalk) walkFrom(ctx context.Context, ref string, tip plumbing.Hash) error {
	commit, err := object.GetCommit(w.st, tip)
	if err != nil {
		// An annotated tag points at a tag object, and a ref may point at a tree or blob: only commits carry history.
		tag, tagErr := object.GetTag(w.st, tip)
		if tagErr != nil {
			return nil
		}
		if commit, err = tag.Commit(); err != nil {
			return nil
		}
	}
	w.ref = ref
	stack := []*object.Commit{commit}
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return errScanBudget
		}
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, seen := w.commits[c.Hash]; seen {
			continue
		}
		w.commits[c.Hash] = struct{}{}
		w.sha = c.Hash.String()
		if err := w.walkTree(ctx, c.TreeHash, ""); err != nil {
			return err
		}
		for i := len(c.ParentHashes) - 1; i >= 0; i-- {
			p, err := object.GetCommit(w.st, c.ParentHashes[i])
			if err != nil {
				continue
			}
			stack = append(stack, p)
		}
	}
	return nil
}

func (w *historyWalk) walkTree(ctx context.Context, h plumbing.Hash, prefix string) error {
	if _, seen := w.trees[h]; seen {
		return nil
	}
	w.trees[h] = struct{}{}
	tree, err := object.GetTree(w.st, h)
	if err != nil {
		return nil
	}
	for _, e := range tree.Entries {
		p := prefix + e.Name
		switch e.Mode {
		case filemode.Dir:
			if err := w.walkTree(ctx, e.Hash, p+"/"); err != nil {
				return err
			}
		case filemode.Regular, filemode.Executable, filemode.Deprecated:
			if err := w.scanBlob(ctx, e.Hash, p); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *historyWalk) scanBlob(ctx context.Context, h plumbing.Hash, path string) error {
	if _, seen := w.blobs[h]; seen {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return errScanBudget
	}
	if len(w.blobs) >= w.svc.cfg.BackfillMaxBlobs {
		return errScanBudget
	}
	w.blobs[h] = struct{}{}
	blob, err := object.GetBlob(w.st, h)
	if err != nil || blob.Size > backfillMaxBlobBytes {
		return nil
	}
	rd, err := blob.Reader()
	if err != nil {
		return nil
	}
	content, err := io.ReadAll(rd)
	_ = rd.Close()
	if err != nil {
		return nil
	}
	for _, f := range secretscan.Scan(path, content, backfillMaxBlobBytes) {
		if err := w.record(ctx, f, path); err != nil {
			return err
		}
	}
	return nil
}

func (w *historyWalk) record(ctx context.Context, f secretscan.Finding, path string) error {
	revoked := false
	if f.Type == model.SecretTypeCloudzillaToken {
		if w.svc.tokens == nil {
			return nil
		}
		live, err := w.svc.tokens.ResolveToken(ctx, *w.repo, f)
		if err != nil {
			return fmt.Errorf("secret scan: resolve token: %w", err)
		}
		if !live {
			return nil
		}
		revoked = true
	}
	loc := model.SecretSighting{CommitSHA: w.sha, FilePath: path, Line: f.Line, Ref: w.ref, Detail: backfillDetail}
	known, err := w.svc.scans.HasLocation(ctx, w.repo.ID, secretscan.Hash(w.repo.ID, f.Value), loc)
	if err != nil {
		return fmt.Errorf("secret scan: check location: %w", err)
	}
	if known {
		return nil
	}
	alert, created, err := w.svc.alerts.Record(ctx, w.repo.ID, f, loc, model.SecretEventFound)
	if err != nil {
		return fmt.Errorf("secret scan: record: %w", err)
	}
	if revoked {
		if err := w.svc.alerts.AutoRevoke(ctx, alert.ID); err != nil {
			return fmt.Errorf("secret scan: close revoked alert: %w", err)
		}
	}
	if created {
		w.opened = append(w.opened, alert)
	}
	return nil
}

// backfillTokenResolver lets the history scan revoke a leaked token the way the push path does.
type backfillTokenResolver struct{ revoker *TokenRevoker }

func (b backfillTokenResolver) ResolveToken(ctx context.Context, repo model.Repository, f secretscan.Finding) (bool, error) {
	return b.revoker.ResolveLiveToken(ctx, &repo, f.Value)
}

func (s *SecretScanService) scanClaimed(parent context.Context, repoID int64) {
	record := context.WithoutCancel(parent)
	repo, err := s.repos.GetByID(record, repoID)
	if err != nil {
		s.fail(record, repoID, err)
		return
	}
	ctx, cancel := context.WithTimeout(parent, s.cfg.BackfillTimeout)
	defer cancel()
	opened, incomplete, err := s.ScanRepo(ctx, repo)
	if parent.Err() != nil {
		return // shutting down: the lease expires and another run rescans from scratch
	}
	if err != nil {
		s.fail(record, repoID, err)
		return
	}
	if len(opened) > 0 && s.notifier != nil {
		s.notifier.NotifySecretAlert(record, *repo, opened[0], len(opened))
	}
	if err := s.scans.Finish(record, repoID, incomplete); err != nil {
		slog.Error("secret scan: record result failed", "repo_id", repoID, "error", err)
	}
}

func (s *SecretScanService) fail(ctx context.Context, repoID int64, err error) {
	slog.Error("secret scan failed", "repo_id", repoID, "error", err)
	if err := s.scans.Fail(ctx, repoID, "The scan failed.", failedScanRetry); err != nil {
		slog.Error("secret scan: record failure failed", "repo_id", repoID, "error", err)
	}
}
