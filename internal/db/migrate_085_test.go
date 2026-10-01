package db_test

import (
	"database/sql"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const orgOwnedReposMigration = "migrations/085_org_owned_repos.sql"

// legacyRepoSchema puts repositories back in its pre-085 shape, where an org
// repo's owner_id named its creator.
func legacyRepoSchema(t *testing.T, db *sql.DB) (userID, orgID int64) {
	t.Helper()
	testutil.Exec(t, db, `DROP INDEX idx_repos_org_name_live`)
	testutil.Exec(t, db, `ALTER TABLE repositories DROP CONSTRAINT repositories_owner_or_org`)
	testutil.Exec(t, db, `ALTER TABLE repositories DROP COLUMN created_by`)
	if err := db.QueryRow(`INSERT INTO users (username, email, password_hash) VALUES ('creator', 'creator@test.invalid', 'x') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO organizations (name) VALUES ('acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	return userID, orgID
}

func runMigration(t *testing.T, db *sql.DB) error {
	t.Helper()
	sql, err := os.ReadFile(orgOwnedReposMigration)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	_, err = db.Exec(string(sql))
	return err
}

func TestOrgOwnedReposMigration_MovesOrgReposOffTheirCreator(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	userID, orgID := legacyRepoSchema(t, db)
	var personal, orgRepo int64
	if err := db.QueryRow(`INSERT INTO repositories (owner_id, owner_name, name) VALUES ($1, 'creator', 'mine') RETURNING id`, userID).Scan(&personal); err != nil {
		t.Fatalf("seed personal repo: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO repositories (owner_id, owner_name, org_id, name) VALUES ($1, 'acme', $2, 'shared') RETURNING id`, userID, orgID).Scan(&orgRepo); err != nil {
		t.Fatalf("seed org repo: %v", err)
	}

	for run := 1; run <= 2; run++ {
		if err := runMigration(t, db); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		for id, want := range map[int64]struct{ owner, org sql.NullInt64 }{
			personal: {owner: sql.NullInt64{Int64: userID, Valid: true}},
			orgRepo:  {org: sql.NullInt64{Int64: orgID, Valid: true}},
		} {
			var owner, org, createdBy sql.NullInt64
			if err := db.QueryRow(`SELECT owner_id, org_id, created_by FROM repositories WHERE id = $1`, id).Scan(&owner, &org, &createdBy); err != nil {
				t.Fatalf("read repo %d: %v", id, err)
			}
			if owner != want.owner || org != want.org || createdBy.Int64 != userID {
				t.Errorf("run %d, repo %d: owner_id %v org_id %v created_by %v; want %v, %v, %d", run, id, owner, org, createdBy, want.owner, want.org, userID)
			}
		}
	}

	// Deleting the creator now leaves the org repo alone.
	testutil.Exec(t, db, `DELETE FROM users WHERE id = $1`, userID)
	var left int
	if err := db.QueryRow(`SELECT COUNT(*) FROM repositories WHERE id = $1 AND created_by IS NULL`, orgRepo).Scan(&left); err != nil || left != 1 {
		t.Errorf("org repo after deleting its creator: %d rows (err %v), want 1 with created_by NULL", left, err)
	}
}

// Before claimRepo, a second org owner could create a live name that
// already existed; only a person can tell which of those rows to keep.
func TestOrgOwnedReposMigration_DuplicateLiveOrgRepos_FailsNamingRepoIDs(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	userID, orgID := legacyRepoSchema(t, db)
	var otherID int64
	if err := db.QueryRow(`INSERT INTO users (username, email, password_hash) VALUES ('other', 'other@test.invalid', 'x') RETURNING id`).Scan(&otherID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	var ids []int64
	for _, creator := range []int64{userID, otherID} {
		var id int64
		if err := db.QueryRow(`INSERT INTO repositories (owner_id, owner_name, org_id, name) VALUES ($1, 'acme', $2, 'twin') RETURNING id`, creator, orgID).Scan(&id); err != nil {
			t.Fatalf("seed org repo: %v", err)
		}
		ids = append(ids, id)
	}

	err := runMigration(t, db)

	if err == nil {
		t.Fatal("migration must fail while an org has two live repos of one name")
	}
	if want := "repo IDs: " + strconv.FormatInt(ids[0], 10) + ", " + strconv.FormatInt(ids[1], 10); !strings.Contains(err.Error(), want) {
		t.Errorf("error must name both repos (%q): %v", want, err)
	}
	var nulled int
	if err := db.QueryRow(`SELECT COUNT(*) FROM repositories WHERE owner_id IS NULL`).Scan(&nulled); err != nil || nulled != 0 {
		t.Errorf("failed migration changed %d rows (err %v)", nulled, err)
	}
}
