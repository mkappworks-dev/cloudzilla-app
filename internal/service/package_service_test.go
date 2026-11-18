package service_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type packageEnv struct {
	db  *sql.DB
	svc *service.PackageService
}

func newPackageEnv(t *testing.T, quota config.QuotaConfig) packageEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	return packageEnv{db: db, svc: service.NewPackageService(store.NewPackageStore(db), store.NewUserStore(db), quota)}
}

func (e packageEnv) seedPackage(t *testing.T, ownerUserID, repoID int64, name string, pending bool) int64 {
	t.Helper()
	var id int64
	query := `INSERT INTO packages (owner_user_id, type, name, repo_id, creator_id) VALUES ($1, 'container', $2, $3, $1) RETURNING id`
	args := []any{ownerUserID, name, repoID}
	if pending {
		query = `INSERT INTO packages (owner_user_id, type, name, creator_id, pending_expires_at)
			VALUES ($1, 'container', $2, $1, NOW() + INTERVAL '1 day') RETURNING id`
		args = args[:2]
	}
	if err := e.db.QueryRow(query, args...).Scan(&id); err != nil {
		t.Fatalf("seed package: %v", err)
	}
	return id
}

func (e packageEnv) seedBlob(t *testing.T, digest string, size int64, pkgIDs ...int64) {
	t.Helper()
	testutil.Exec(t, e.db, `INSERT INTO registry_blobs (digest, size) VALUES ($1, $2) ON CONFLICT DO NOTHING`, digest, size)
	t.Cleanup(func() {
		testutil.Exec(t, e.db, `DELETE FROM package_blobs WHERE digest = $1`, digest)
		testutil.Exec(t, e.db, `DELETE FROM registry_blobs WHERE digest = $1`, digest)
	})
	for _, id := range pkgIDs {
		testutil.Exec(t, e.db, `INSERT INTO package_blobs (package_id, digest) VALUES ($1, $2)`, id, digest)
	}
}

func uniqueDigest(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("sha256:%064s", strings.ReplaceAll(testutil.UniqueSuffix(t), "_", "a"))
}

func TestPackageService_UsageCountsASharedBlobOncePerOwner(t *testing.T) {
	e := newPackageEnv(t, config.QuotaConfig{})
	suffix := testutil.UniqueSuffix(t)
	alice, bob := testutil.SeedUser(t, e.db, "a"+suffix), testutil.SeedUser(t, e.db, "b"+suffix)
	aliceRepo := testutil.SeedRepo(t, e.db, alice, "alice", "a"+suffix)
	bobRepo := testutil.SeedRepo(t, e.db, bob, "bob", "b"+suffix)
	one, two := e.seedPackage(t, alice, aliceRepo, "one", false), e.seedPackage(t, alice, aliceRepo, "two", false)
	other := e.seedPackage(t, bob, bobRepo, "one", false)
	e.seedBlob(t, uniqueDigest(t), 100, one, two, other)
	e.seedBlob(t, uniqueDigest(t), 30, one)

	aliceUsed, err := e.svc.Usage(context.Background(), service.QuotaOwner{UserID: alice})
	if err != nil {
		t.Fatal(err)
	}
	bobUsed, err := e.svc.Usage(context.Background(), service.QuotaOwner{UserID: bob})
	if err != nil {
		t.Fatal(err)
	}

	if aliceUsed != 130 {
		t.Errorf("alice: shared blob once plus her own = 130, got %d", aliceUsed)
	}
	if bobUsed != 100 {
		t.Errorf("bob pays for the same blob too = 100, got %d", bobUsed)
	}
}

func TestPackageService_UsageIncludesPendingPackagesGenericFilesAndInFlightBytes(t *testing.T) {
	e := newPackageEnv(t, config.QuotaConfig{})
	suffix := testutil.UniqueSuffix(t)
	alice := testutil.SeedUser(t, e.db, "a"+suffix)
	repo := testutil.SeedRepo(t, e.db, alice, "alice", "a"+suffix)
	pending := e.seedPackage(t, alice, 0, "fresh", true)
	e.seedBlob(t, uniqueDigest(t), 7, pending)

	var generic, version int64
	if err := e.db.QueryRow(`INSERT INTO packages (owner_user_id, type, name, repo_id, creator_id) VALUES ($1, 'generic', 'tool', $2, $1) RETURNING id`, alice, repo).Scan(&generic); err != nil {
		t.Fatal(err)
	}
	if err := e.db.QueryRow(`INSERT INTO package_versions (package_id, version) VALUES ($1, '1.0') RETURNING id`, generic).Scan(&version); err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, e.db, `INSERT INTO package_files (version_id, filename, size, sha256, storage_key) VALUES ($1, 'a.tgz', 40, 'x', 'k1'), ($1, 'b.tgz', 50, 'y', 'k2')`, version)
	testutil.Exec(t, e.db, `INSERT INTO package_upload_sessions (id, package_id, user_id, bytes, temp_path) VALUES (gen_random_uuid(), $1, $2, 300, '/tmp/x')`, pending, alice)

	used, err := e.svc.Usage(context.Background(), service.QuotaOwner{UserID: alice})
	if err != nil {
		t.Fatal(err)
	}

	if want := int64(7 + 40 + 50 + 300); used != want {
		t.Errorf("pending blob 7 + files 90 + in-flight 300 = %d, got %d", want, used)
	}
}

func TestPackageService_UsageOfAnOrgCountsOnlyItsOwnPackages(t *testing.T) {
	e := newPackageEnv(t, config.QuotaConfig{})
	suffix := testutil.UniqueSuffix(t)
	alice := testutil.SeedUser(t, e.db, "a"+suffix)
	var orgID, pkg int64
	if err := e.db.QueryRow(`INSERT INTO organizations (name) VALUES ($1) RETURNING id`, "org_"+suffix).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	testutil.DeleteOrgOnCleanup(t, e.db, orgID)
	if err := e.db.QueryRow(`INSERT INTO packages (owner_org_id, type, name, creator_id, pending_expires_at)
		VALUES ($1, 'container', 'img', $2, NOW() + INTERVAL '1 day') RETURNING id`, orgID, alice).Scan(&pkg); err != nil {
		t.Fatal(err)
	}
	e.seedBlob(t, uniqueDigest(t), 11, pkg)

	org, err := e.svc.Usage(context.Background(), service.QuotaOwner{OrgID: orgID})
	if err != nil {
		t.Fatal(err)
	}
	user, err := e.svc.Usage(context.Background(), service.QuotaOwner{UserID: alice})
	if err != nil {
		t.Fatal(err)
	}

	if org != 11 || user != 0 {
		t.Errorf("org = %d, user = %d; want 11 and 0", org, user)
	}
}

func TestPackageService_CheckQuota(t *testing.T) {
	cfg := config.QuotaConfig{User: config.QuotaLimits{PackageBytes: 100}}
	e := newPackageEnv(t, cfg)
	suffix := testutil.UniqueSuffix(t)
	alice := testutil.SeedUser(t, e.db, "a"+suffix)
	root := testutil.SeedSuperadmin(t, e.db, "r"+suffix)
	repo := testutil.SeedRepo(t, e.db, alice, "alice", "a"+suffix)
	pkg := e.seedPackage(t, alice, repo, "img", false)
	e.seedBlob(t, uniqueDigest(t), 80, pkg)
	ctx := context.Background()
	owner := service.QuotaOwner{UserID: alice}

	if err := e.svc.CheckQuota(ctx, owner, 20); err != nil {
		t.Errorf("exactly reaching the quota is allowed: %v", err)
	}
	err := e.svc.CheckQuota(ctx, owner, 21)
	var qe *service.PackageQuotaError
	if !errors.Is(err, service.ErrPackageQuota) || !errors.As(err, &qe) {
		t.Fatalf("want ErrPackageQuota, got %v", err)
	}
	if qe.Used != 80 || qe.Limit != 100 {
		t.Errorf("usage and quota = %d of %d, want 80 of 100", qe.Used, qe.Limit)
	}
	if want := "package storage quota reached (80 B of 100 B)"; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
	if err := e.svc.CheckQuota(ctx, service.QuotaOwner{UserID: root}, 1<<40); err != nil {
		t.Errorf("a superadmin's personal account is unlimited, as for git storage: %v", err)
	}
}

func TestPackageService_CheckQuotaOffByDefault(t *testing.T) {
	e := newPackageEnv(t, config.QuotaConfig{})
	alice := testutil.SeedUser(t, e.db, "a"+testutil.UniqueSuffix(t))

	if err := e.svc.CheckQuota(context.Background(), service.QuotaOwner{UserID: alice}, 1<<50); err != nil {
		t.Errorf("a zero quota is off: %v", err)
	}
}
