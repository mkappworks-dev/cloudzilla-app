package db_test

import (
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/db"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

var registryTables = []string{
	"package_files", "package_versions", "package_upload_sessions", "package_tags", "package_manifest_children",
	"package_manifest_blobs", "package_manifests", "package_blobs", "registry_blobs", "packages",
}

func TestRegistryMigration_AppliesOnADBAtThePreviousVersion(t *testing.T) {
	d := testutil.OpenFreshTestDB(t)
	owner := seedNamedUser(t, d, "carol")
	testutil.Exec(t, d, `INSERT INTO repositories (owner_id, owner_name, name) VALUES ($1, 'carol', 'kept')`, owner)
	for _, table := range registryTables {
		testutil.Exec(t, d, `DROP TABLE `+table+` CASCADE`)
	}
	testutil.Exec(t, d, `DELETE FROM schema_migrations WHERE version >= '142'`)

	if err := db.Migrate(d); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var repos, packages int
	if err := d.QueryRow(`SELECT (SELECT COUNT(*) FROM repositories), (SELECT COUNT(*) FROM packages)`).Scan(&repos, &packages); err != nil {
		t.Fatal(err)
	}
	if repos != 1 || packages != 0 {
		t.Errorf("repositories = %d, packages = %d; want the existing repo kept and an empty packages table", repos, packages)
	}
}

func TestRegistryMigration_PackageNamesAreUniquePerPathOwner(t *testing.T) {
	d := testutil.OpenFreshTestDB(t)
	owner := seedNamedUser(t, d, "dave")
	var repoID int64
	if err := d.QueryRow(`INSERT INTO repositories (owner_id, owner_name, name) VALUES ($1, 'dave', 'app') RETURNING id`, owner).Scan(&repoID); err != nil {
		t.Fatal(err)
	}
	insert := func(repo any, creator int64, expires any) error {
		_, err := d.Exec(`INSERT INTO packages (owner_user_id, type, name, repo_id, creator_id, pending_expires_at)
			VALUES ($1, 'container', 'app', $2, $3, $4)`, owner, repo, creator, expires)
		return err
	}

	if err := insert(repoID, owner, nil); err != nil {
		t.Fatalf("linked package: %v", err)
	}
	if err := insert(repoID, owner, nil); err == nil {
		t.Error("two linked packages with one path owner and name must violate the unique index")
	}
	if err := insert(nil, owner, "2099-01-01"); err != nil {
		t.Errorf("a pending package may share a linked package's name: %v", err)
	}
	if err := insert(nil, owner, "2099-01-01"); err == nil {
		t.Error("two pending packages of one creator and name must violate the unique index")
	}
	if err := insert(nil, owner, nil); err == nil {
		t.Error("a pending package without an expiry must be refused")
	}
}

func TestRegistryMigration_DeletingARepoRemovesItsPackagesAndLinks(t *testing.T) {
	d := testutil.OpenFreshTestDB(t)
	owner := seedNamedUser(t, d, "erin")
	var repoID, pkgID int64
	if err := d.QueryRow(`INSERT INTO repositories (owner_id, owner_name, name) VALUES ($1, 'erin', 'app') RETURNING id`, owner).Scan(&repoID); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRow(`INSERT INTO packages (owner_user_id, type, name, repo_id, creator_id)
		VALUES ($1, 'container', 'app', $2, $1) RETURNING id`, owner, repoID).Scan(&pkgID); err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testutil.Exec(t, d, `INSERT INTO registry_blobs (digest, size) VALUES ($1, 10)`, digest)
	testutil.Exec(t, d, `INSERT INTO package_blobs (package_id, digest) VALUES ($1, $2)`, pkgID, digest)

	testutil.Exec(t, d, `DELETE FROM repositories WHERE id = $1`, repoID)

	var packages, links, blobs int
	if err := d.QueryRow(`SELECT (SELECT COUNT(*) FROM packages), (SELECT COUNT(*) FROM package_blobs), (SELECT COUNT(*) FROM registry_blobs)`).
		Scan(&packages, &links, &blobs); err != nil {
		t.Fatal(err)
	}
	if packages != 0 || links != 0 || blobs != 1 {
		t.Errorf("packages=%d links=%d blobs=%d; want the package and its links gone and the shared blob left for the sweep", packages, links, blobs)
	}
}
