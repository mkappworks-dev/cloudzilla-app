package service

import (
	"context"
	"database/sql"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/secretscan"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const (
	awsKey      = "AKIAIOSFODNN7EXAMPLE"
	awsKeyOther = "AKIAJ5ZQ4XW7N3PBDKLM"
)

func putObject(t *testing.T, st storage.Storer, o object.Object) plumbing.Hash {
	t.Helper()
	enc := st.NewEncodedObject()
	if err := o.Encode(enc); err != nil {
		t.Fatal(err)
	}
	h, err := st.SetEncodedObject(enc)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func putBlob(t *testing.T, st storage.Storer, content string) plumbing.Hash {
	t.Helper()
	enc := st.NewEncodedObject()
	enc.SetType(plumbing.BlobObject)
	w, _ := enc.Writer()
	_, _ = w.Write([]byte(content))
	_ = w.Close()
	h, err := st.SetEncodedObject(enc)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// commitFiles writes a commit whose flat tree holds files, on top of parents.
func commitFiles(t *testing.T, st storage.Storer, files map[string]string, parents ...plumbing.Hash) plumbing.Hash {
	t.Helper()
	var entries []object.TreeEntry
	for name, content := range files {
		entries = append(entries, object.TreeEntry{Name: name, Mode: filemode.Regular, Hash: putBlob(t, st, content)})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	tree := putObject(t, st, &object.Tree{Entries: entries})
	sig := object.Signature{Name: "T", Email: "t@example.com", When: time.Unix(0, 0).UTC()}
	return putObject(t, st, &object.Commit{Author: sig, Committer: sig, Message: "c", TreeHash: tree, ParentHashes: parents})
}

func setRef(t *testing.T, st storage.Storer, name string, h plumbing.Hash) {
	t.Helper()
	if err := st.SetReference(plumbing.NewHashReference(plumbing.ReferenceName(name), h)); err != nil {
		t.Fatal(err)
	}
}

type digestRecorder struct {
	mu     sync.Mutex
	counts []int
}

func (d *digestRecorder) NotifySecretAlert(_ context.Context, _ model.Repository, _ *model.SecretScanAlert, count int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.counts = append(d.counts, count)
}

type scanFixture struct {
	svc    *SecretScanService
	alerts *SecretAlertService
	notes  *digestRecorder
	repo   *model.Repository
	stores *store.Stores
	db     *sql.DB
}

func newScanFixture(t *testing.T, cfg config.SecretScanningConfig) *scanFixture {
	t.Helper()
	db := testutil.OpenFreshTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	owner := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, owner, "owner"+suffix, suffix)
	stores := store.New(db)
	repo, err := stores.Repo.GetByID(context.Background(), repoID)
	if err != nil {
		t.Fatal(err)
	}
	alerts := NewSecretAlertService(stores.SecretAlert)
	notes := &digestRecorder{}
	svc := NewSecretScanService(stores.SecretScan, stores.Repo, alerts, notes, config.GitConfig{}, cfg)
	return &scanFixture{svc: svc, alerts: alerts, notes: notes, repo: repo, stores: stores, db: db}
}

var defaultScanCfg = config.SecretScanningConfig{BackfillTimeout: time.Minute, BackfillMaxBlobs: 1000}

func TestBackfill_OldCommitOnSideBranchGetsOneAlertAndASecondRunAddsNothing(t *testing.T) {
	f := newScanFixture(t, defaultScanCfg)
	st := memory.NewStorage()
	old := commitFiles(t, st, map[string]string{".env": "AWS_KEY=" + awsKey + "\n"})
	side := commitFiles(t, st, map[string]string{"README": "clean"}, old)
	main := commitFiles(t, st, map[string]string{"README": "clean"})
	setRef(t, st, "refs/heads/side", side)
	setRef(t, st, "refs/heads/main", main)
	ctx := context.Background()

	for run := 1; run <= 2; run++ {
		w, err := f.svc.scanHistory(ctx, f.repo, st)
		if err != nil || w.incomplete {
			t.Fatalf("run %d: err=%v incomplete=%v", run, err, w.incomplete)
		}
		if want := map[int]int{1: 1, 2: 0}[run]; len(w.opened) != want {
			t.Fatalf("run %d opened %d alerts, want %d", run, len(w.opened), want)
		}
	}
	list, total, err := f.alerts.List(ctx, f.repo.ID, "", "", 1)
	if err != nil || total != 1 {
		t.Fatalf("alerts total=%d err=%v", total, err)
	}
	events, _ := f.alerts.Events(ctx, list[0].ID)
	if len(events) != 1 || events[0].Kind != model.SecretEventFound || events[0].Detail != backfillDetail {
		t.Fatalf("events = %+v", events)
	}
	locs, _ := f.alerts.Locations(ctx, list[0].ID)
	if len(locs) != 1 || !locs[0].ReachedRef || locs[0].FilePath != ".env" {
		t.Fatalf("locations = %+v", locs)
	}
}

func TestBackfill_FileUnchangedAcross10000CommitsIsReadOnce(t *testing.T) {
	f := newScanFixture(t, config.SecretScanningConfig{BackfillTimeout: time.Minute, BackfillMaxBlobs: 1000})
	st := memory.NewStorage()
	var prev []plumbing.Hash
	for i := 0; i < 10000; i++ {
		files := map[string]string{"stable.txt": "same content\n", "v.txt": []string{"a\n", "b\n"}[i%2]}
		prev = []plumbing.Hash{commitFiles(t, st, files, prev...)}
	}
	setRef(t, st, "refs/heads/main", prev[0])

	w, err := f.svc.scanHistory(context.Background(), f.repo, st)
	if err != nil || w.incomplete {
		t.Fatalf("err=%v incomplete=%v", err, w.incomplete)
	}
	if len(w.blobs) != 3 {
		t.Fatalf("read %d distinct blobs, want 3", len(w.blobs))
	}
}

func TestBackfill_BlobBudgetMarksIncompleteAndKeepsFindings(t *testing.T) {
	f := newScanFixture(t, config.SecretScanningConfig{BackfillTimeout: time.Minute, BackfillMaxBlobs: 2})
	st := memory.NewStorage()
	c := commitFiles(t, st, map[string]string{"a": "x1", "b": "AWS=" + awsKey, "c": "x3", "d": "x4"})
	setRef(t, st, "refs/heads/main", c)

	w, err := f.svc.scanHistory(context.Background(), f.repo, st)
	if err != nil || !w.incomplete {
		t.Fatalf("err=%v incomplete=%v", err, w.incomplete)
	}
	if len(w.blobs) != 2 {
		t.Fatalf("blobs = %d, want the budget of 2", len(w.blobs))
	}
}

func TestBackfill_CloudzillaTokenDroppedWithoutResolver(t *testing.T) {
	f := newScanFixture(t, defaultScanCfg)
	st := memory.NewStorage()
	tok := "czp_" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	setRef(t, st, "refs/heads/main", commitFiles(t, st, map[string]string{"t": "TOKEN=" + tok + "\n"}))
	w, err := f.svc.scanHistory(context.Background(), f.repo, st)
	if err != nil || len(w.opened) != 0 {
		t.Fatalf("err=%v opened=%d", err, len(w.opened))
	}
}

type tokenOutcomes struct{ outcome TokenOutcome }

func (l tokenOutcomes) ResolveToken(context.Context, *model.Repository, string) (TokenOutcome, error) {
	return l.outcome, nil
}

func TestBackfill_ExpiredCloudzillaTokenFilesAClosedRevokedAlert(t *testing.T) {
	f := newScanFixture(t, defaultScanCfg)
	f.svc.WithTokenResolver(tokenOutcomes{TokenExpired})
	st := memory.NewStorage()
	tok := "czp_" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	setRef(t, st, "refs/heads/main", commitFiles(t, st, map[string]string{"t": "TOKEN=" + tok + "\n"}))

	if _, err := f.svc.scanHistory(context.Background(), f.repo, st); err != nil {
		t.Fatal(err)
	}

	got, _, err := f.alerts.List(context.Background(), f.repo.ID, "", "", 1)
	if err != nil || len(got) != 1 {
		t.Fatalf("err=%v alerts=%+v, want one", err, got)
	}
	if a := got[0]; a.State != "closed" || a.Resolution != model.SecretResolutionRevoked || a.AutoRevoked {
		t.Errorf("alert = %+v, want closed revoked, not auto_revoked", a)
	}
}

func TestBackfill_UnknownCloudzillaTokenIsDropped(t *testing.T) {
	f := newScanFixture(t, defaultScanCfg)
	f.svc.WithTokenResolver(tokenOutcomes{TokenUnknown})
	st := memory.NewStorage()
	tok := "czp_" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	setRef(t, st, "refs/heads/main", commitFiles(t, st, map[string]string{"t": "TOKEN=" + tok + "\n"}))

	if _, err := f.svc.scanHistory(context.Background(), f.repo, st); err != nil {
		t.Fatal(err)
	}

	if got, _, _ := f.alerts.List(context.Background(), f.repo.ID, "", "", 1); len(got) != 0 {
		t.Errorf("alerts = %+v, want none", got)
	}
}

func TestBackfill_MaxBlobBytesIsHonoured(t *testing.T) {
	cfg := defaultScanCfg
	cfg.MaxBlobBytes = 16
	f := newScanFixture(t, cfg)
	st := memory.NewStorage()
	setRef(t, st, "refs/heads/main", commitFiles(t, st, map[string]string{".env": "AWS_KEY=" + awsKey + "\n"}))

	w, err := f.svc.scanHistory(context.Background(), f.repo, st)

	if err != nil || len(w.opened) != 0 {
		t.Fatalf("err=%v opened=%d, want a blob over max_blob_bytes skipped", err, len(w.opened))
	}
}

func TestBackfill_LiveCloudzillaTokenIsRecorded(t *testing.T) {
	f := newScanFixture(t, defaultScanCfg)
	f.svc.WithTokenResolver(tokenOutcomes{TokenRevoked})
	st := memory.NewStorage()
	tok := "czp_" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	setRef(t, st, "refs/heads/main", commitFiles(t, st, map[string]string{"t": "TOKEN=" + tok + "\n"}))
	w, err := f.svc.scanHistory(context.Background(), f.repo, st)
	if err != nil || len(w.opened) != 1 || w.opened[0].SecretType != model.SecretTypeCloudzillaToken {
		t.Fatalf("err=%v opened=%v", err, w.opened)
	}
	got, err := f.alerts.Get(context.Background(), f.repo.ID, w.opened[0].Number)
	if err != nil || got.State != "closed" || !got.AutoRevoked {
		t.Fatalf("a live token's alert must close as auto-revoked: err=%v alert=%+v", err, got)
	}
}

func TestBackfill_ScanClaimedSendsOneDigestAndFinishes(t *testing.T) {
	f := newScanFixture(t, defaultScanCfg)
	// ScanRepo opens the bare repo from disk; scanClaimed on a missing one must fail the scan, not finish it.
	ctx := context.Background()
	if err := f.stores.SecretScan.MarkPending(ctx, f.repo.ID); err != nil {
		t.Fatal(err)
	}
	id, ok, err := f.stores.SecretScan.Claim(ctx, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim: id=%d ok=%v err=%v", id, ok, err)
	}
	f.svc.scanClaimed(ctx, id)
	state, _, _ := f.svc.ScanState(ctx, f.repo.ID)
	if state != "failed" {
		t.Fatalf("state = %q, want failed for a repository with no git directory", state)
	}
}

func TestSecretScanStore_ClaimNeverHandsOneRepoToTwoClaimers(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	owner := testutil.SeedUser(t, db, suffix)
	testutil.SeedRepo(t, db, owner, "owner"+suffix, suffix)
	s := store.New(db).SecretScan
	ctx := context.Background()

	var wg sync.WaitGroup
	var mu sync.Mutex
	claimed := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok, err := s.Claim(ctx, time.Minute); err == nil && ok {
				mu.Lock()
				claimed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if claimed != 1 {
		t.Fatalf("%d claimers got the one repo, want 1", claimed)
	}
	if _, ok, _ := s.Claim(ctx, time.Minute); ok {
		t.Fatal("a leased repo was claimed again")
	}
}

func TestSecretScanStore_PendingLifecycle(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	owner := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, owner, "owner"+suffix, suffix)
	s := store.New(db).SecretScan
	ctx := context.Background()

	if state, _, _ := s.State(ctx, repoID); state != "pending" {
		t.Fatalf("a new repository starts %q, want pending", state)
	}
	if _, ok, _ := s.Claim(ctx, time.Minute); !ok {
		t.Fatal("claim")
	}
	if err := s.Finish(ctx, repoID, true); err != nil {
		t.Fatal(err)
	}
	if state, inc, _ := s.State(ctx, repoID); state != "done" || !inc {
		t.Fatalf("after finish: %q incomplete=%v", state, inc)
	}
	if _, ok, _ := s.Claim(ctx, time.Minute); ok {
		t.Fatal("a done scan was claimed")
	}

	// A sync that lands while a scan runs must not be lost when the scan finishes.
	_ = s.MarkPending(ctx, repoID)
	_, _, _ = s.Claim(ctx, time.Minute)
	_ = s.MarkPending(ctx, repoID)
	_ = s.Finish(ctx, repoID, false)
	if state, _, _ := s.State(ctx, repoID); state != "pending" {
		t.Fatalf("a mid-scan MarkPending ended %q, want pending", state)
	}
	if _, ok, _ := s.Claim(ctx, time.Minute); !ok {
		t.Fatal("the re-queued scan was not claimable once the first one finished")
	}
}

func TestSecretScanStore_FailedRetriesAfterItsDelay(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	owner := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, owner, "owner"+suffix, suffix)
	s := store.New(db).SecretScan
	ctx := context.Background()
	_, _, _ = s.Claim(ctx, time.Minute)
	_ = s.Fail(ctx, repoID, "boom", time.Hour)
	if _, ok, _ := s.Claim(ctx, time.Minute); ok {
		t.Fatal("a failed scan was retried before its delay")
	}
	testutil.Exec(t, db, `UPDATE repo_secret_scans SET lease_until = NOW() - INTERVAL '1 second' WHERE repo_id = $1`, repoID)
	if _, ok, _ := s.Claim(ctx, time.Minute); !ok {
		t.Fatal("a failed scan was not retried after its delay")
	}
}

func TestBackfill_SweepDeletesOnlyBlockedAlertsPastSevenDays(t *testing.T) {
	f := newScanFixture(t, defaultScanCfg)
	ctx := context.Background()
	blocked, _, err := f.alerts.Record(ctx, f.repo.ID, secretscan.Finding{Type: "aws_access_key_id", Line: 1, Value: awsKey, Confidence: secretscan.High},
		model.SecretSighting{CommitSHA: "aaa", FilePath: ".env"}, model.SecretEventBlocked)
	if err != nil {
		t.Fatal(err)
	}
	reached, _, _ := f.alerts.Record(ctx, f.repo.ID, secretscan.Finding{Type: "aws_access_key_id", Line: 1, Value: awsKeyOther, Confidence: secretscan.High},
		model.SecretSighting{CommitSHA: "bbb", FilePath: ".env"}, model.SecretEventFound)
	testutil.Exec(t, f.db, `UPDATE secret_scan_alerts SET created_at = NOW() - INTERVAL '8 days' WHERE repo_id = $1`, f.repo.ID)

	f.svc.sweepBlocked(ctx)
	if _, err := f.alerts.Get(ctx, f.repo.ID, blocked.Number); err == nil {
		t.Fatal("the old blocked alert survived the sweep")
	}
	if _, err := f.alerts.Get(ctx, f.repo.ID, reached.Number); err != nil {
		t.Fatalf("the reached alert was swept: %v", err)
	}
}
