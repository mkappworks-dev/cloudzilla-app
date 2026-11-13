package db_test

import (
	"os"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestRepoAliasesMigration_FreshDBHasTheTable(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	owner := seedNamedUser(t, db, "alice")
	var repoID int64
	if err := db.QueryRow(`INSERT INTO repositories (owner_id, owner_name, name) VALUES ($1, 'alice', 'mine') RETURNING id`, owner).Scan(&repoID); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	testutil.Exec(t, db, `INSERT INTO repo_aliases (repo_id, owner_name, name) VALUES ($1, 'alice', 'Old')`, repoID)

	if _, err := db.Exec(`INSERT INTO repo_aliases (repo_id, owner_name, name) VALUES ($1, 'ALICE', 'old')`, repoID); err == nil {
		t.Error("two aliases sharing a path, ignoring case, must violate the unique index")
	}

	testutil.Exec(t, db, `DELETE FROM repositories WHERE id = $1`, repoID)
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM repo_aliases`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("deleting the repo left %d aliases, want the cascade to remove them", n)
	}
}

func TestRepoAliasesMigration_AppliesOnPopulatedDB(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	// Seeded before the drop: the owner-name trigger on users (migration 129) reads repo_aliases.
	owner := seedNamedUser(t, db, "bob")
	for _, name := range []string{"one", "two"} {
		testutil.Exec(t, db, `INSERT INTO repositories (owner_id, owner_name, name) VALUES ($1, 'bob', $2)`, owner, name)
	}
	testutil.Exec(t, db, `DROP TABLE repo_aliases`)

	migration, err := os.ReadFile("migrations/122_repo_aliases.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := db.Exec(string(migration)); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var repos, aliases int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM repositories), (SELECT COUNT(*) FROM repo_aliases)`).Scan(&repos, &aliases); err != nil {
		t.Fatal(err)
	}
	if repos != 2 || aliases != 0 {
		t.Errorf("repositories = %d, repo_aliases = %d; want 2 and an empty table", repos, aliases)
	}
}
