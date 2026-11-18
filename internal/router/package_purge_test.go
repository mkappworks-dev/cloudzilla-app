package router_test

// What a Repository purge and an owner deletion take with them. Requires TEST_DATABASE_DSN and skips otherwise.

import (
	"bytes"
	"net/http"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func (e *genericEnv) packagesNamed(t *testing.T, names ...string) int {
	t.Helper()
	n := 0
	for _, name := range names {
		n += e.count(t, `SELECT COUNT(*) FROM packages WHERE name = $1`, name)
	}
	return n
}

// seedGeneric adds a generic package at ownerUserID's or ownerOrgID's path linked to repoID, with one
// stored object.
func (e *genericEnv) seedGeneric(t *testing.T, ownerUserID, ownerOrgID, repoID int64, name string) {
	t.Helper()
	backend, err := storage.NewLocal(e.root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	key := "packages/generic/repo/" + name + ".bin"
	if err := backend.Put(t.Context(), key, bytes.NewReader([]byte("x")), 1); err != nil {
		t.Fatal(err)
	}
	var userID, orgID, pkg, version any
	if ownerOrgID != 0 {
		orgID = ownerOrgID
	} else {
		userID = ownerUserID
	}
	if err := e.db.QueryRow(`INSERT INTO packages (owner_user_id, owner_org_id, type, name, repo_id, creator_id)
		VALUES ($1, $2, 'generic', $3, $4, $5) RETURNING id`, userID, orgID, name, repoID, e.ownerID).Scan(&pkg); err != nil {
		t.Fatal(err)
	}
	if err := e.db.QueryRow(`INSERT INTO package_versions (package_id, version) VALUES ($1, '1') RETURNING id`, pkg).Scan(&version); err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, e.db, `INSERT INTO package_files (version_id, filename, size, sha256, storage_key) VALUES ($1, 'f', 1, 'x', $2)`, version, key)
}

func TestPurge_ARepositoryTakesOnlyItsOwnPackagesAndTheirObjects(t *testing.T) {
	e := newGenericEnv(t, 1<<20, 0)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	img := e.pushBlobs(t, auth, e.pubImage, nil)
	e.putManifest(auth, e.pubImage, "v1", mtOCIManifest, img.manifest)
	if rr := e.put(e.uploadURL("pub", "tool", "1", "f.bin"), e.ownerPAT, true, []byte("pub file")); rr.Code != http.StatusCreated {
		t.Fatalf("upload to pub: %d %s", rr.Code, rr.Body)
	}
	if rr := e.put(e.uploadURL("priv", "kept", "1", "f.bin"), e.ownerPAT, true, []byte("priv file")); rr.Code != http.StatusCreated {
		t.Fatalf("upload to priv: %d %s", rr.Code, rr.Body)
	}
	before := e.objects(t)
	if err := e.svc.Repo.Delete(t.Context(), e.pubRepoID, e.ownerID); err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, e.db, `UPDATE repositories SET deleted_at = $2 WHERE id = $1`, e.pubRepoID, time.Now().Add(-31*24*time.Hour))

	if err := e.svc.Repo.PurgeExpired(t.Context()); err != nil {
		t.Fatal(err)
	}

	if n := e.packagesNamed(t, "pubimg", "tool"); n != 0 {
		t.Errorf("%d packages of the purged repository remain", n)
	}
	if n := e.packagesNamed(t, "privimg", "kept"); n != 2 {
		t.Errorf("packages of the other repository = %d, want both kept", n)
	}
	if got := e.objects(t); got != before-1 {
		t.Errorf("objects = %d, want %d: the generic file goes at once, the image's blobs wait for the sweep", got, before-1)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM package_blobs WHERE digest = $1`, digestOf(img.layer)); n != 0 {
		t.Error("the purged image's blob link remains, so the sweep could never reclaim the blob")
	}
	if dl := e.get(e.downloadURL("priv", "kept", "1", "f.bin"), e.ownerPAT, true); dl.Code != http.StatusOK {
		t.Errorf("the other repository's file = %d, want 200", dl.Code)
	}
}

func TestPurge_DeletingAUserTakesEverythingUnderTheirPathEvenAfterATransfer(t *testing.T) {
	e := newGenericEnv(t, 1<<20, 0)
	auth := e.pushAuth(t, e.ownerPAT, e.pubImage)
	img := e.pushBlobs(t, auth, e.pubImage, nil)
	e.putManifest(auth, e.pubImage, "v1", mtOCIManifest, img.manifest)
	strangerRepo := testutil.SeedRepo(t, e.db, e.strangerID, e.stranger, testutil.UniqueSuffix(t))
	e.seedGeneric(t, e.ownerID, 0, strangerRepo, "transferred")
	e.seedGeneric(t, e.ownerID, 0, e.pubRepoID, "own")
	testutil.Exec(t, e.db, `INSERT INTO packages (owner_user_id, type, name, creator_id, pending_expires_at)
		VALUES ($1, 'container', 'half', $1, NOW() + INTERVAL '1 day')`, e.ownerID)
	before := e.objects(t)

	if err := e.svc.User.DeleteUser(t.Context(), e.ownerID); err != nil {
		t.Fatal(err)
	}

	if n := e.packagesNamed(t, "pubimg", "privimg", "transferred", "own", "half"); n != 0 {
		t.Errorf("%d packages under the deleted path remain", n)
	}
	if got := e.objects(t); got != before-2 {
		t.Errorf("objects = %d, want %d: both generic files go, blobs wait for the sweep", got, before-2)
	}
}

func TestPurge_DeletingAnOrganizationTakesItsPackagesAndObjects(t *testing.T) {
	e := newGenericEnv(t, 1<<20, 0)
	org, err := e.svc.Org.Create(t.Context(), e.ownerID, "org"+testutil.UniqueSuffix(t), "", "")
	if err != nil {
		t.Fatal(err)
	}
	testutil.DeleteOrgOnCleanup(t, e.db, org.ID)
	// An organization with repositories can't be deleted, so what it has under its path links to a repository that moved away.
	e.seedGeneric(t, 0, org.ID, e.pubRepoID, "orgfile")
	e.seedGeneric(t, e.ownerID, 0, e.pubRepoID, "personal")
	before := e.objects(t)

	if err := e.svc.Org.Delete(t.Context(), org.ID, e.ownerID); err != nil {
		t.Fatal(err)
	}

	if n := e.packagesNamed(t, "orgfile"); n != 0 {
		t.Error("the organization's package remains")
	}
	if n := e.packagesNamed(t, "personal"); n != 1 {
		t.Error("an unrelated package went with the organization")
	}
	if got := e.objects(t); got != before-1 {
		t.Errorf("objects = %d, want %d", got, before-1)
	}
}
