package service_test

// The registry sweep. Each test gets its own schema because a sweep is global: it would reap other tests' blobs.

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const gcGrace = 24 * time.Hour

// failingDelete is a backend whose Delete fails while fail is set.
type failingDelete struct {
	storage.Backend
	fail bool
}

func (b *failingDelete) Delete(ctx context.Context, key string) error {
	if b.fail {
		return errors.New("object store down")
	}
	return b.Backend.Delete(ctx, key)
}

type gcEnv struct {
	db      *sql.DB
	gc      *service.RegistryGCService
	reg     *store.RegistryStore
	backend *failingDelete
	spool   string
	now     time.Time
	userID  int64
	repoID  int64
	pkgID   int64
}

func newGCEnv(t *testing.T) *gcEnv {
	t.Helper()
	db := testutil.OpenFreshTestDB(t)
	local, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = local.Close() })
	e := &gcEnv{db: db, reg: store.NewRegistryStore(db), backend: &failingDelete{Backend: local}, spool: t.TempDir(), now: time.Now().UTC()}
	e.gc = service.NewRegistryGCService(e.reg, config.RegistryConfig{GCGrace: gcGrace, UploadTTL: gcGrace}).
		WithBackend(e.backend).WithClock(func() time.Time { return e.now })
	suffix := testutil.UniqueSuffix(t)
	e.userID = testutil.SeedUser(t, db, "u"+suffix)
	e.repoID = testutil.SeedRepo(t, db, e.userID, "u"+suffix, suffix)
	e.pkgID = e.seedPackage(t, "app")
	return e
}

func (e *gcEnv) seedPackage(t *testing.T, name string) int64 {
	t.Helper()
	var id int64
	err := e.db.QueryRow(`INSERT INTO packages (owner_user_id, type, name, repo_id, creator_id)
		VALUES ($1, 'container', $2, $3, $1) RETURNING id`, e.userID, name, e.repoID).Scan(&id)
	if err != nil {
		t.Fatalf("seed package: %v", err)
	}
	return id
}

var digestSeq int

func (e *gcEnv) newDigest() string {
	digestSeq++
	return fmt.Sprintf("sha256:%064x", digestSeq)
}

func objectKey(digest string) string { return "registry/" + strings.TrimPrefix(digest, "sha256:") }

// blob stores an object and its row, linked to each package at linkedAt.
func (e *gcEnv) blob(t *testing.T, linkedAt time.Time, pkgIDs ...int64) string {
	t.Helper()
	d := e.newDigest()
	if err := e.backend.Put(t.Context(), objectKey(d), bytes.NewReader([]byte("x")), 1); err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, e.db, `INSERT INTO registry_blobs (digest, size) VALUES ($1, 1)`, d)
	for _, id := range pkgIDs {
		testutil.Exec(t, e.db, `INSERT INTO package_blobs (package_id, digest, created_at) VALUES ($1, $2, $3)`, id, d, linkedAt)
	}
	return d
}

// manifest records a manifest of pkgID naming blobs, as a push does.
func (e *gcEnv) manifest(t *testing.T, pkgID int64, blobs ...string) int64 {
	t.Helper()
	var id int64
	err := e.db.QueryRow(`INSERT INTO package_manifests (package_id, digest, media_type, size, content)
		VALUES ($1, $2, 'application/vnd.oci.image.manifest.v1+json', 1, 'x') RETURNING id`, pkgID, e.newDigest()).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range blobs {
		testutil.Exec(t, e.db, `INSERT INTO package_manifest_blobs (manifest_id, digest) VALUES ($1, $2)`, id, b)
	}
	return id
}

func (e *gcEnv) hasObject(t *testing.T, digest string) bool {
	t.Helper()
	ok, err := e.backend.Exists(t.Context(), objectKey(digest))
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func (e *gcEnv) hasRow(t *testing.T, digest string) bool {
	t.Helper()
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM registry_blobs WHERE digest = $1`, digest).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

func (e *gcEnv) sweepAt(t *testing.T, at time.Time) {
	t.Helper()
	e.now = at
	if err := e.gc.Sweep(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
}

func TestRegistryGC_LayersOfAnAbortedPushGoAfterTwoGraces(t *testing.T) {
	e := newGCEnv(t)
	pushed := e.now.Add(-time.Hour)
	layer := e.blob(t, pushed, e.pkgID)

	e.sweepAt(t, pushed.Add(gcGrace-time.Minute))
	if !e.hasObject(t, layer) {
		t.Fatal("blob deleted before the link's grace ended")
	}

	e.sweepAt(t, pushed.Add(gcGrace+time.Minute))
	if !e.hasObject(t, layer) || !e.hasRow(t, layer) {
		t.Fatal("blob deleted in the same sweep that unlinked it; it must wait a second grace")
	}

	e.sweepAt(t, pushed.Add(2*gcGrace+time.Minute))
	if e.hasObject(t, layer) || e.hasRow(t, layer) {
		t.Errorf("blob still stored after two graces: object %v row %v", e.hasObject(t, layer), e.hasRow(t, layer))
	}
}

func TestRegistryGC_ADeletedImagesBlobsGoAfterTheGraceAndASharedLayerStays(t *testing.T) {
	e := newGCEnv(t)
	other := e.seedPackage(t, "other")
	old := e.now.Add(-10 * 24 * time.Hour)
	own := e.blob(t, old, e.pkgID)
	shared := e.blob(t, old, e.pkgID, other)
	e.manifest(t, other, shared)
	kept := e.blob(t, old, e.pkgID)
	e.manifest(t, e.pkgID, kept)

	e.sweepAt(t, e.now)
	e.sweepAt(t, e.now.Add(gcGrace-time.Minute))
	if !e.hasObject(t, own) {
		t.Fatal("blob of a deleted image went before the grace after its unlinking")
	}
	e.sweepAt(t, e.now.Add(gcGrace+time.Minute))

	if e.hasObject(t, own) || e.hasRow(t, own) {
		t.Error("an unreferenced blob is still stored")
	}
	if !e.hasObject(t, shared) || !e.hasRow(t, shared) {
		t.Error("a layer another package still links was deleted")
	}
	if !e.hasObject(t, kept) {
		t.Error("a layer a manifest still references was deleted")
	}
}

func TestRegistryGC_ABlobRelinkedBeforeTheDeleteSurvivesWithItsObject(t *testing.T) {
	e := newGCEnv(t)
	layer := e.blob(t, e.now.Add(-2*gcGrace), e.pkgID)
	e.sweepAt(t, e.now)
	late := e.now.Add(gcGrace + time.Minute)

	e.gc.WithSweepHook(func(digest string) {
		if digest != layer {
			return
		}
		linked, err := e.reg.LinkExistingBlob(t.Context(), e.seedPackage(t, "racer"), digest)
		if err != nil || !linked {
			t.Errorf("link in the race window: linked %v, err %v", linked, err)
		}
	})
	e.sweepAt(t, late)

	if !e.hasObject(t, layer) || !e.hasRow(t, layer) {
		t.Errorf("re-linked blob lost: object %v row %v", e.hasObject(t, layer), e.hasRow(t, layer))
	}
}

func TestRegistryGC_ABlobLinkedWithoutClearingItsMarkIsKept(t *testing.T) {
	e := newGCEnv(t)
	layer := e.blob(t, e.now.Add(-2*gcGrace))
	e.sweepAt(t, e.now)
	late := e.now.Add(gcGrace + time.Minute)
	testutil.Exec(t, e.db, `INSERT INTO package_blobs (package_id, digest, created_at) VALUES ($1, $2, $3)`, e.pkgID, layer, late)

	e.sweepAt(t, late)

	if !e.hasObject(t, layer) || !e.hasRow(t, layer) {
		t.Error("a blob with a link was deleted")
	}
}

func TestRegistryGC_AFailedObjectDeleteLeavesTheRowForTheNextRun(t *testing.T) {
	e := newGCEnv(t)
	layer := e.blob(t, e.now.Add(-2*gcGrace), e.pkgID)
	e.sweepAt(t, e.now)
	late := e.now.Add(gcGrace + time.Minute)

	e.backend.fail = true
	e.sweepAt(t, late)
	if !e.hasRow(t, layer) || !e.hasObject(t, layer) {
		t.Fatal("row or object lost although the object delete failed")
	}

	e.backend.fail = false
	e.sweepAt(t, late.Add(time.Hour))
	if e.hasRow(t, layer) || e.hasObject(t, layer) {
		t.Error("the next run did not finish the delete")
	}
}

func TestRegistryGC_ExpiredPendingPackagesAndSessionsAreReclaimed(t *testing.T) {
	e := newGCEnv(t)
	spool := func(name string) string {
		p := filepath.Join(e.spool, name)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	var pending, live int64
	for _, c := range []struct {
		name    string
		expires string
		id      *int64
	}{{"gone", "-1 hour", &pending}, {"fresh", "+1 hour", &live}} {
		err := e.db.QueryRow(`INSERT INTO packages (owner_user_id, type, name, creator_id, pending_expires_at)
			VALUES ($1, 'container', $2, $1, NOW() + $3::interval) RETURNING id`, e.userID, c.name, c.expires).Scan(c.id)
		if err != nil {
			t.Fatal(err)
		}
	}
	pendingFile, liveFile, staleFile := spool("pending"), spool("live"), spool("stale")
	for _, s := range []struct {
		id, path string
		pkg      int64
		created  time.Time
	}{
		{"11111111-1111-1111-1111-111111111111", pendingFile, pending, e.now},
		{"22222222-2222-2222-2222-222222222222", liveFile, live, e.now},
		{"33333333-3333-3333-3333-333333333333", staleFile, e.pkgID, e.now.Add(-gcGrace - time.Minute)},
	} {
		testutil.Exec(t, e.db, `INSERT INTO package_upload_sessions (id, package_id, user_id, temp_path, created_at)
			VALUES ($1, $2, $3, $4, $5)`, s.id, s.pkg, e.userID, s.path, s.created)
	}
	// The pending package's blob link expires with it, leaving the blob to the unlinked sweep.
	layer := e.blob(t, e.now, pending)

	e.sweepAt(t, e.now.Add(time.Minute))

	var gone, stayed int
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM packages WHERE id = $1`, pending).Scan(&gone)
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM packages WHERE id = $1`, live).Scan(&stayed)
	if gone != 0 || stayed != 1 {
		t.Errorf("expired pending package rows = %d, unexpired = %d; want 0 and 1", gone, stayed)
	}
	for path, wantGone := range map[string]bool{pendingFile: true, staleFile: true, liveFile: false} {
		if _, err := os.Stat(path); (err != nil) != wantGone {
			t.Errorf("spool file %s: gone = %v, want %v", filepath.Base(path), err != nil, wantGone)
		}
	}
	var sessions int
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM package_upload_sessions`).Scan(&sessions)
	if sessions != 1 {
		t.Errorf("sessions left = %d, want only the live one", sessions)
	}
	if !e.hasRow(t, layer) {
		t.Fatal("the blob went in the sweep that dropped its link")
	}
	e.sweepAt(t, e.now.Add(gcGrace+2*time.Minute))
	if e.hasRow(t, layer) || e.hasObject(t, layer) {
		t.Error("the expired package's blob was never reclaimed")
	}
}

func TestRegistryGC_ASweepBatchesPastFiveHundredBlobs(t *testing.T) {
	e := newGCEnv(t)
	old := e.now.Add(-2 * gcGrace)
	var digests []string
	for range 520 {
		digests = append(digests, e.blob(t, old, e.pkgID))
	}

	e.sweepAt(t, e.now)
	e.sweepAt(t, e.now.Add(gcGrace+time.Minute))

	for _, d := range []string{digests[0], digests[519]} {
		if e.hasRow(t, d) || e.hasObject(t, d) {
			t.Errorf("blob %s survived a sweep of 520 candidates", d[:15])
		}
	}
}

func TestRegistryGC_ReUploadingALinkedBlobRestartsItsGrace(t *testing.T) {
	e := newGCEnv(t)
	layer := e.blob(t, time.Now().Add(-2*gcGrace), e.pkgID)

	if linked, err := e.reg.LinkExistingBlob(t.Context(), e.pkgID, layer); err != nil || !linked {
		t.Fatalf("re-link: linked %v, err %v", linked, err)
	}
	e.sweepAt(t, time.Now().Add(time.Minute))

	var links int
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM package_blobs WHERE digest = $1`, layer).Scan(&links)
	if links != 1 {
		t.Error("a retried push's link was swept before its manifest could name it")
	}
}
