package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/storer"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/secretscan"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// ErrSecretCheckFailed is all a pusher learns of a failure checking a push for secrets: go-git sends a vet error's
// text to the pusher.
var ErrSecretCheckFailed = errors.New("internal error checking for secrets")

const postReceiveMarkBatch = 1000

// SecretAlertNotifier tells repository admins about an alert a scan after the push opened. Ticket 11 implements it.
type SecretAlertNotifier interface {
	NotifySecretAlert(ctx context.Context, repo *model.Repository, alert *model.SecretScanAlert)
}

// CloudzillaTokenResolver decides whether a czp_ value is a live token. Ticket 14 implements it, revoking the token it
// finds; without one every token is treated as not live and dropped.
type CloudzillaTokenResolver interface {
	ResolveLiveToken(ctx context.Context, repo *model.Repository, value string) (live bool, err error)
}

// SecretScanFunc is secretscan.Scan with a failure mode, so a broken scanner can be told apart from a clean blob.
type SecretScanFunc func(path string, content []byte, maxBytes int) ([]secretscan.Finding, error)

// PushScanService scans what a push adds for secrets, before the refs move (push protection) and after.
type PushScanService struct {
	alerts   *SecretAlertService
	cfg      config.SecretScanningConfig
	baseURL  string
	scan     SecretScanFunc
	notifier SecretAlertNotifier
	tokens   CloudzillaTokenResolver

	postReceiveDone func()
}

func NewPushScanService(alerts *SecretAlertService, cfg config.SecretScanningConfig, baseURL string) *PushScanService {
	d := defaultSecretScanning()
	if cfg.MaxBlobBytes < 1 {
		cfg.MaxBlobBytes = d.MaxBlobBytes
	}
	if cfg.PushTimeout <= 0 {
		cfg.PushTimeout = d.PushTimeout
	}
	if cfg.MaxBlobs < 1 {
		cfg.MaxBlobs = d.MaxBlobs
	}
	return &PushScanService{
		alerts:  alerts,
		cfg:     cfg,
		baseURL: strings.TrimRight(baseURL, "/"),
		scan: func(path string, content []byte, maxBytes int) ([]secretscan.Finding, error) {
			return secretscan.Scan(path, content, maxBytes), nil
		},
	}
}

// defaultSecretScanning backs the limits a hand-built Config leaves at zero; config.Load always sets them.
func defaultSecretScanning() config.SecretScanningConfig {
	return config.SecretScanningConfig{MaxBlobBytes: 1 << 20, PushTimeout: 10 * time.Second, MaxBlobs: 5000}
}

func (s *PushScanService) WithNotifier(n SecretAlertNotifier) *PushScanService {
	s.notifier = n
	return s
}

// singleAlertNotifier adapts NotificationService, whose digest form takes a count, to one alert at a time.
type singleAlertNotifier struct{ n *NotificationService }

func (a singleAlertNotifier) NotifySecretAlert(ctx context.Context, repo *model.Repository, alert *model.SecretScanAlert) {
	a.n.NotifySecretAlert(ctx, *repo, alert, 1)
}

func (s *PushScanService) WithTokenResolver(r CloudzillaTokenResolver) *PushScanService {
	s.tokens = r
	return s
}

// WithPostReceiveDone calls f when each scan after a push ends; tests use it to wait for the background work.
func (s *PushScanService) WithPostReceiveDone(f func()) *PushScanService {
	s.postReceiveDone = f
	return s
}

// WithScanner replaces the scanner; tests use it to count scans and to make one fail or stall.
func (s *PushScanService) WithScanner(f SecretScanFunc) *PushScanService {
	s.scan = f
	return s
}

// SecretDetectedError is the one line a pusher sees for a refused ref.
type SecretDetectedError struct {
	Type, Path string
	Line, More int
	URL        string
	// Revoked marks a leaked Cloudzilla token the instance has deleted; there is nothing to allow.
	Revoked bool
}

func (e *SecretDetectedError) Error() string {
	more := ""
	if e.More > 0 {
		more = fmt.Sprintf(" and %d more", e.More)
	}
	if e.Revoked {
		return fmt.Sprintf("secret detected: Cloudzilla access token in %s:%d%s (the token was revoked); see %s",
			e.Path, e.Line, more, e.URL)
	}
	return fmt.Sprintf("secret detected: %s in %s:%d%s; if it is not a live secret, a writer can allow it at %s",
		e.Type, e.Path, e.Line, more, e.URL)
}

type pathFinding struct {
	secretscan.Finding
	Path string
}

func (f pathFinding) key(commit plumbing.Hash) string {
	return fmt.Sprintf("%s|%s|%d|%s", commit, f.Path, f.Line, f.Type)
}

// PushScan is the secret scan of one push, shared by every ref the push updates. Its methods are not safe for
// concurrent use: receive-pack vets refs one at a time, and PostReceive runs after the last.
type PushScan struct {
	svc      *PushScanService
	repo     *model.Repository
	store    storer.Storer
	pusherID int64

	tips []plumbing.Hash
	// commits holds the commits no ref reached before the push, parents first.
	commits []*object.Commit
	byHash  map[plumbing.Hash]*object.Commit
	walked  bool
	// scanned holds the commits all of whose changed blobs were scanned.
	scanned   map[plumbing.Hash]bool
	findings  map[plumbing.Hash][]pathFinding
	blobs     map[plumbing.Hash][]secretscan.Finding
	blobsRead int
	// recorded holds the alerts a blocked push already wrote, so a second ref sharing a commit does not repeat them.
	recorded map[string]*model.SecretScanAlert
	// revokedHashes remembers the tokens this push deleted, which no longer resolve, so their other locations are recorded.
	revokedHashes map[string]bool

	incomplete bool
	reason     string
	deadline   time.Time
	// budgeted is false for the scan after the push, which only the context bounds.
	budgeted bool
}

// Begin starts the scan of one push to gitRepo. Call Expect with the push's commands before the first ref is vetted.
func (s *PushScanService) Begin(repo *model.Repository, gitRepo *gogit.Repository, pusherID int64) *PushScan {
	return &PushScan{
		svc:           s,
		repo:          repo,
		store:         gitRepo.Storer,
		pusherID:      pusherID,
		byHash:        make(map[plumbing.Hash]*object.Commit),
		scanned:       make(map[plumbing.Hash]bool),
		findings:      make(map[plumbing.Hash][]pathFinding),
		blobs:         make(map[plumbing.Hash][]secretscan.Finding),
		recorded:      make(map[string]*model.SecretScanAlert),
		revokedHashes: make(map[string]bool),
		budgeted:      true,
	}
}

// ForCommit is the scan of a commit made in the browser: nothing has scanned it yet, and it has one parent at most.
func (s *PushScanService) ForCommit(repo *model.Repository, gitRepo *gogit.Repository, pusherID int64, commit plumbing.Hash) *PushScan {
	p := s.Begin(repo, gitRepo, pusherID)
	p.walked = true
	if c, err := object.GetCommit(p.store, commit); err == nil {
		p.commits = []*object.Commit{c}
		p.byHash[c.Hash] = c
	}
	return p
}

// Expect names the commands of the push, so the walk covers every ref's new value at once.
func (p *PushScan) Expect(cmds []*packp.Command) {
	p.tips = p.tips[:0]
	for _, cmd := range cmds {
		if cmd.Action() != packp.Delete {
			p.tips = append(p.tips, cmd.New)
		}
	}
}

// Check refuses a ref whose new commits add a secret that push protection blocks. It fails open (nil) when the scan
// cannot finish; PostReceive then scans what is left.
func (p *PushScan) Check(ctx context.Context, cmd *packp.Command) error {
	if cmd.Action() == packp.Delete {
		return nil
	}
	if len(p.tips) == 0 {
		p.tips = []plumbing.Hash{cmd.New}
	}
	// The walk runs even with protection off: once the refs move, what the push added can no longer be told apart.
	p.walk()
	if !p.svc.cfg.PushProtection {
		return nil
	}
	if !p.incomplete {
		p.scanCommits(ctx, p.commits)
	}

	tip, ok := peelToCommit(p.store, cmd.New)
	if !ok {
		return nil
	}
	var blocking []blockedFinding
	seen := make(map[string]bool)
	for _, c := range p.reachable([]plumbing.Hash{tip}) {
		for _, f := range p.findings[c.Hash] {
			if f.Confidence != secretscan.High {
				continue
			}
			block, revoked := p.blocks(ctx, cmd, c.Hash, f)
			if !block {
				continue
			}
			hash := secretscan.Hash(p.repo.ID, f.Value)
			if revoked {
				blocking = append(blocking, blockedFinding{c.Hash, f, true})
				continue
			}
			k := fmt.Sprintf("%s|%s|%d", hash, f.Path, f.Line)
			if seen[k] {
				continue
			}
			seen[k] = true
			blocking = append(blocking, blockedFinding{c.Hash, f, false})
		}
	}
	if len(blocking) == 0 {
		return nil
	}
	first, err := p.recordBlocked(ctx, cmd, blocking[0])
	if err != nil {
		return err
	}
	for _, b := range blocking[1:] {
		if _, err := p.recordBlocked(ctx, cmd, b); err != nil {
			return err
		}
	}
	return &SecretDetectedError{
		Type: first.SecretType, Path: blocking[0].Path, Line: blocking[0].Line, More: len(blocking) - 1,
		Revoked: blocking[0].revoked,
		URL:     fmt.Sprintf("%s/%s/%s/security/secrets/%d", p.svc.baseURL, p.repo.OwnerName, p.repo.Name, first.Number),
	}
}

// blocks reports whether push protection refuses the high-confidence finding f; revoked marks a token deleted just now.
// A failed read fails open: it is logged and the scan marked incomplete, and PostReceive settles f.
func (p *PushScan) blocks(ctx context.Context, cmd *packp.Command, commit plumbing.Hash, f pathFinding) (block, revoked bool) {
	revoked, handled, err := p.revokeLeakedToken(ctx, cmd, commit, f)
	if err != nil {
		p.stop("revoke token", err)
		return false, false
	}
	if handled {
		return revoked, revoked
	}
	live, err := p.worthBlocking(ctx, f)
	if err != nil {
		p.stop("resolve token", err)
		return false, false
	}
	if !live {
		return false, false
	}
	closed, err := p.svc.alerts.IsClosed(ctx, p.repo.ID, secretscan.Hash(p.repo.ID, f.Value))
	if err != nil {
		p.stop("look up alert", err)
		return false, false
	}
	return !closed, false
}

type blockedFinding struct {
	commit plumbing.Hash
	pathFinding
	revoked bool
}

func (p *PushScan) recordBlocked(ctx context.Context, cmd *packp.Command, b blockedFinding) (*model.SecretScanAlert, error) {
	k := b.key(b.commit)
	if alert, ok := p.recorded[k]; ok {
		return alert, nil
	}
	alert, _, err := p.svc.alerts.Record(ctx, p.repo.ID, b.Finding, model.SecretSighting{
		CommitSHA: b.commit.String(), FilePath: b.Path, Ref: cmd.Name.String(), ActorID: p.pusherID,
	}, model.SecretEventBlocked)
	if err != nil {
		slog.Error("push-protection: record alert", "repo_id", p.repo.ID, "error", err)
		return nil, ErrSecretCheckFailed
	}
	if b.revoked {
		if err := p.svc.alerts.AutoRevoke(ctx, alert.ID); err != nil && !errors.Is(err, store.ErrSecretAlertState) {
			slog.Error("push-protection: close revoked alert", "repo_id", p.repo.ID, "error", err)
			return nil, ErrSecretCheckFailed
		}
	}
	p.recorded[k] = alert
	return alert, nil
}

// revokeLeakedToken settles a czp_ finding when the resolver can tell expired from unknown. handled is false for any
// other finding; revoked means the token was deleted just now and its alert is still to be recorded and closed.
func (p *PushScan) revokeLeakedToken(ctx context.Context, cmd *packp.Command, commit plumbing.Hash, f pathFinding) (revoked, handled bool, err error) {
	if f.Type != model.SecretTypeCloudzillaToken {
		return false, false, nil
	}
	r, ok := p.svc.tokens.(TokenOutcomeResolver)
	if !ok {
		return false, false, nil
	}
	hash := secretscan.Hash(p.repo.ID, f.Value)
	if p.revokedHashes[hash] {
		return true, true, nil
	}
	sg := model.SecretSighting{CommitSHA: commit.String(), FilePath: f.Path, Ref: cmd.Name.String(), ActorID: p.pusherID}
	out, err := p.settleToken(ctx, r, f, sg, model.SecretEventBlocked)
	if out == TokenRevoked && err == nil {
		p.revokedHashes[hash] = true
	}
	return out == TokenRevoked, true, err
}

// settleToken resolves f and, but for a revoke, files the alert closed: a closed alert only gains the location, and an
// expired token's alert closes as revoked with no user. A revoke is left for the caller to record.
func (p *PushScan) settleToken(ctx context.Context, r TokenOutcomeResolver, f pathFinding, sg model.SecretSighting, kind string) (TokenOutcome, error) {
	hash := secretscan.Hash(p.repo.ID, f.Value)
	closed, err := p.svc.alerts.IsClosed(ctx, p.repo.ID, hash)
	if err != nil {
		return TokenUnknown, err
	}
	if closed {
		_, _, err := p.svc.alerts.Record(ctx, p.repo.ID, f.Finding, sg, kind)
		return TokenUnknown, err
	}
	out, err := r.ResolveToken(ctx, p.repo, f.Value)
	if err != nil || out == TokenUnknown || out == TokenRevoked {
		return out, err
	}
	alert, _, err := p.svc.alerts.Record(ctx, p.repo.ID, f.Finding, sg, kind)
	if err != nil {
		return out, err
	}
	if err := p.svc.alerts.Close(ctx, alert.ID, 0, model.SecretResolutionRevoked); err != nil && !errors.Is(err, store.ErrSecretAlertState) {
		return out, err
	}
	return TokenExpired, nil
}

// worthBlocking drops a czp_ value that is not a live token.
func (p *PushScan) worthBlocking(ctx context.Context, f pathFinding) (bool, error) {
	if f.Type != model.SecretTypeCloudzillaToken {
		return true, nil
	}
	if p.svc.tokens == nil {
		return false, nil
	}
	return p.svc.tokens.ResolveLiveToken(ctx, p.repo, f.Value)
}

// walk finds the commits the push adds, once per push.
func (p *PushScan) walk() {
	if p.walked {
		return
	}
	p.walked = true
	p.deadline = time.Now().Add(p.svc.cfg.PushTimeout)
	commits, err := gittransport.AddedCommits(p.store, p.tips)
	if err != nil {
		p.stop("walk failed", err)
		return
	}
	p.commits = parentsFirstCommits(commits)
	for _, c := range p.commits {
		p.byHash[c.Hash] = c
	}
}

// scanCommits scans the commits not yet scanned, in order, until the budget or a scanner failure ends it; what it
// did not reach is left for PostReceive.
func (p *PushScan) scanCommits(ctx context.Context, commits []*object.Commit) {
	for _, c := range commits {
		if p.scanned[c.Hash] {
			continue
		}
		if !p.scanCommit(ctx, c) {
			return
		}
		p.scanned[c.Hash] = true
	}
}

func (p *PushScan) scanCommit(ctx context.Context, c *object.Commit) bool {
	// A commit a stopped scan left half done is scanned again from the start.
	delete(p.findings, c.Hash)
	tree, err := c.Tree()
	if err != nil {
		p.stop("read tree", err)
		return false
	}
	var bases []*object.Tree
	for _, ph := range c.ParentHashes {
		if pc, err := object.GetCommit(p.store, ph); err == nil {
			if t, err := pc.Tree(); err == nil {
				bases = append(bases, t)
			}
		}
	}
	return p.scanTree(ctx, c.Hash, tree, bases, "")
}

// scanTree scans the blobs tree adds over bases, the same trees one level down in the commit's parents.
func (p *PushScan) scanTree(ctx context.Context, commit plumbing.Hash, tree *object.Tree, bases []*object.Tree, prefix string) bool {
	known := make(map[plumbing.Hash]bool)
	for _, b := range bases {
		if b.Hash == tree.Hash {
			return true
		}
		for _, e := range b.Entries {
			if e.Mode != filemode.Submodule {
				known[e.Hash] = e.Mode == filemode.Dir
			}
		}
	}
	for _, e := range tree.Entries {
		if isDir, ok := known[e.Hash]; ok && isDir == (e.Mode == filemode.Dir) {
			continue
		}
		switch e.Mode {
		case filemode.Submodule, filemode.Symlink:
		case filemode.Dir:
			sub, err := object.GetTree(p.store, e.Hash)
			if err != nil {
				p.stop("read tree", err)
				return false
			}
			var subBases []*object.Tree
			for _, b := range bases {
				for _, be := range b.Entries {
					if be.Name == e.Name && be.Mode == filemode.Dir {
						if bt, err := object.GetTree(p.store, be.Hash); err == nil {
							subBases = append(subBases, bt)
						}
					}
				}
			}
			if !p.scanTree(ctx, commit, sub, subBases, prefix+e.Name+"/") {
				return false
			}
		default:
			if !p.scanBlob(ctx, commit, prefix+e.Name, e.Hash) {
				return false
			}
		}
	}
	return true
}

// scanBlob reports false when the budget or the scanner stops the scan.
func (p *PushScan) scanBlob(ctx context.Context, commit plumbing.Hash, path string, h plumbing.Hash) bool {
	fs, ok := p.blobs[h]
	if !ok {
		if p.budgeted {
			switch {
			case ctx.Err() != nil || time.Now().After(p.deadline):
				p.stop("timeout", nil)
				return false
			case p.blobsRead >= p.svc.cfg.MaxBlobs:
				p.stop("max_blobs", nil)
				return false
			}
		} else if ctx.Err() != nil {
			p.stop("canceled", ctx.Err())
			return false
		}
		obj, err := p.store.EncodedObject(plumbing.BlobObject, h)
		if err != nil {
			p.stop("read blob", err)
			return false
		}
		if obj.Size() > int64(p.svc.cfg.MaxBlobBytes) {
			p.blobs[h] = nil
			return true
		}
		content, err := readBlob(obj)
		if err != nil {
			p.stop("read blob", err)
			return false
		}
		p.blobsRead++
		if fs, err = p.svc.scan(path, content, p.svc.cfg.MaxBlobBytes); err != nil {
			p.stop("scanner error", err)
			return false
		}
		p.blobs[h] = fs
	}
	for _, f := range fs {
		p.findings[commit] = append(p.findings[commit], pathFinding{Finding: f, Path: path})
	}
	return true
}

func readBlob(obj plumbing.EncodedObject) ([]byte, error) {
	r, err := obj.Reader()
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	return io.ReadAll(r)
}

func (p *PushScan) stop(reason string, err error) {
	if p.incomplete {
		return
	}
	p.incomplete = true
	p.reason = reason
	attrs := []any{"repo_id", p.repo.ID, "reason", reason, "blobs", p.blobsRead,
		"commits_scanned", len(p.scanned), "commits_total", len(p.commits)}
	if err != nil {
		attrs = append(attrs, "error", err)
	}
	slog.Warn("push-protection: scan incomplete", attrs...)
}

// reachable returns the added commits the tips reach, parents first.
func (p *PushScan) reachable(tips []plumbing.Hash) []*object.Commit {
	in := make(map[plumbing.Hash]bool)
	stack := append([]plumbing.Hash(nil), tips...)
	for len(stack) > 0 {
		h := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		c, ok := p.byHash[h]
		if !ok || in[h] {
			continue
		}
		in[h] = true
		stack = append(stack, c.ParentHashes...)
	}
	var out []*object.Commit
	for _, c := range p.commits {
		if in[c.Hash] {
			out = append(out, c)
		}
	}
	return out
}

// PostReceive scans the applied commits that push protection did not finish or did not run on, records what the
// push let through, and marks every applied commit as reached by a ref.
func (p *PushScan) PostReceive(ctx context.Context, commands []*packp.Command) {
	if p.svc.postReceiveDone != nil {
		defer p.svc.postReceiveDone()
	}
	var tips []plumbing.Hash
	refOf := make(map[plumbing.Hash]string)
	for _, cmd := range commands {
		if cmd.Action() == packp.Delete {
			continue
		}
		if tip, ok := peelToCommit(p.store, cmd.New); ok {
			tips = append(tips, tip)
			if _, dup := refOf[tip]; !dup {
				refOf[tip] = cmd.Name.String()
			}
		}
	}
	applied := p.reachable(tips)
	if len(applied) == 0 {
		return
	}
	p.budgeted = false
	p.scanCommits(ctx, applied)

	repoID := p.repo.ID
	for _, c := range applied {
		if len(p.findings[c.Hash]) == 0 {
			continue
		}
		ref := p.refFor(c, refOf)
		for _, f := range p.findings[c.Hash] {
			if !p.recordAfterPush(ctx, c, f, ref) {
				return
			}
		}
	}
	shas := make([]string, 0, len(applied))
	for _, c := range applied {
		shas = append(shas, c.Hash.String())
	}
	for len(shas) > 0 {
		n := min(len(shas), postReceiveMarkBatch)
		if err := p.svc.alerts.MarkReached(ctx, repoID, shas[:n]); err != nil {
			slog.Error("secret-scan: mark reached", "repo_id", repoID, "error", err)
			return
		}
		shas = shas[n:]
	}
}

// refFor is the ref of an applied command that reaches c.
func (p *PushScan) refFor(c *object.Commit, refOf map[plumbing.Hash]string) string {
	refs := make([]string, 0, len(refOf))
	for tip, ref := range refOf {
		for _, r := range p.reachable([]plumbing.Hash{tip}) {
			if r.Hash == c.Hash {
				refs = append(refs, ref)
				break
			}
		}
	}
	sort.Strings(refs)
	if len(refs) == 0 {
		return ""
	}
	return refs[0]
}

// recordAfterPush records one finding of an applied commit; it reports false when the database fails.
func (p *PushScan) recordAfterPush(ctx context.Context, c *object.Commit, f pathFinding, ref string) bool {
	if f.Type == model.SecretTypeCloudzillaToken {
		if r, ok := p.svc.tokens.(TokenOutcomeResolver); ok {
			return p.revokeAfterPush(ctx, r, c, f, ref)
		}
	}
	live, err := p.worthBlocking(ctx, f)
	if err != nil {
		slog.Error("secret-scan: resolve token", "repo_id", p.repo.ID, "error", err)
		return true
	}
	if !live {
		return true
	}
	alert, created, err := p.svc.alerts.Record(ctx, p.repo.ID, f.Finding, model.SecretSighting{
		CommitSHA: c.Hash.String(), FilePath: f.Path, Ref: ref, ActorID: p.pusherID, Incomplete: p.incomplete,
	}, model.SecretEventFound)
	if err != nil {
		slog.Error("secret-scan: record alert", "repo_id", p.repo.ID, "error", err)
		return false
	}
	if created && p.svc.notifier != nil {
		p.svc.notifier.NotifySecretAlert(ctx, p.repo, alert)
	}
	return true
}

// revokeAfterPush settles one czp_ finding of an applied commit; it reports false when the database fails.
func (p *PushScan) revokeAfterPush(ctx context.Context, r TokenOutcomeResolver, c *object.Commit, f pathFinding, ref string) bool {
	sg := model.SecretSighting{CommitSHA: c.Hash.String(), FilePath: f.Path, Ref: ref, ActorID: p.pusherID, Incomplete: p.incomplete}
	out, err := p.settleToken(ctx, r, f, sg, model.SecretEventFound)
	if err != nil {
		slog.Error("secret-scan: settle token", "repo_id", p.repo.ID, "error", err)
		return false
	}
	if out != TokenRevoked {
		return true
	}
	alert, _, err := p.svc.alerts.Record(ctx, p.repo.ID, f.Finding, sg, model.SecretEventFound)
	if err == nil {
		err = p.svc.alerts.AutoRevoke(ctx, alert.ID)
		if errors.Is(err, store.ErrSecretAlertState) {
			err = nil
		}
	}
	if err != nil {
		slog.Error("secret-scan: record revoked token", "repo_id", p.repo.ID, "error", err)
		return false
	}
	return true
}

// peelToCommit follows tags from h to the commit they name.
func peelToCommit(s storer.EncodedObjectStorer, h plumbing.Hash) (plumbing.Hash, bool) {
	for range 16 {
		if _, err := object.GetCommit(s, h); err == nil {
			return h, true
		}
		tag, err := object.GetTag(s, h)
		if err != nil {
			return plumbing.ZeroHash, false
		}
		h = tag.Target
	}
	return plumbing.ZeroHash, false
}

// parentsFirstCommits orders cs so that each commit follows its parents among them.
func parentsFirstCommits(cs []*object.Commit) []*object.Commit {
	in := make(map[plumbing.Hash]*object.Commit, len(cs))
	for _, c := range cs {
		in[c.Hash] = c
	}
	done := make(map[plumbing.Hash]bool, len(cs))
	out := make([]*object.Commit, 0, len(cs))
	for _, c := range cs {
		stack := []*object.Commit{c}
		for len(stack) > 0 {
			top := stack[len(stack)-1]
			if done[top.Hash] {
				stack = stack[:len(stack)-1]
				continue
			}
			ready := true
			for _, ph := range top.ParentHashes {
				if pc, ok := in[ph]; ok && !done[ph] {
					stack = append(stack, pc)
					ready = false
				}
			}
			if ready {
				done[top.Hash] = true
				out = append(out, top)
				stack = stack[:len(stack)-1]
			}
		}
	}
	return out
}
