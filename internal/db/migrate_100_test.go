package db_test

import (
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const dropOwnerPermissionRoleMigration = "migrations/100_drop_owner_permission_role.sql"

func seedNamedUser(t *testing.T, db *sql.DB, name string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(
		`INSERT INTO users (username, email, password_hash) VALUES ($1, $1 || '@test.invalid', 'x') RETURNING id`, name,
	).Scan(&id); err != nil {
		t.Fatalf("seed user %s: %v", name, err)
	}
	return id
}

func insertPermission(db *sql.DB, repoID, userID int64, role string) error {
	_, err := db.Exec(`INSERT INTO permissions (repo_id, user_id, role) VALUES ($1, $2, $3)`, repoID, userID, role)
	return err
}

type grant struct{ repo, user int64 }

func permissionRoles(t *testing.T, db *sql.DB) map[grant]string {
	t.Helper()
	rows, err := db.Query(`SELECT repo_id, user_id, role FROM permissions`)
	if err != nil {
		t.Fatalf("read permissions: %v", err)
	}
	defer rows.Close()
	roles := map[grant]string{}
	for rows.Next() {
		var g grant
		var role string
		if err := rows.Scan(&g.repo, &g.user, &role); err != nil {
			t.Fatalf("scan permission: %v", err)
		}
		roles[g] = role
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read permissions: %v", err)
	}
	return roles
}

func TestPermissionsRole_RejectsOwner(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	ownerID := seedNamedUser(t, db, "alice")
	var repoID int64
	if err := db.QueryRow(`INSERT INTO repositories (owner_id, owner_name, name) VALUES ($1, 'alice', 'mine') RETURNING id`, ownerID).Scan(&repoID); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	for _, role := range []string{"reader", "writer", "admin"} {
		if err := insertPermission(db, repoID, seedNamedUser(t, db, role+"_user"), role); err != nil {
			t.Errorf("insert role %q: %v", role, err)
		}
	}

	err := insertPermission(db, repoID, seedNamedUser(t, db, "would_be_owner"), "owner")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != "permissions_role_check" {
		t.Errorf("insert role 'owner': err = %v, want a permissions_role_check violation", err)
	}
}

func TestDropOwnerPermissionRoleMigration_DeletesOwnersRowsAndDowngradesTheRest(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	testutil.Exec(t, db, `ALTER TABLE permissions DROP CONSTRAINT permissions_role_check`)
	testutil.Exec(t, db, `ALTER TABLE permissions ADD CONSTRAINT permissions_role_check CHECK (role IN ('owner','admin','writer','reader'))`)

	alice := seedNamedUser(t, db, "alice")
	orgOwner := seedNamedUser(t, db, "orgowner")
	orgMember := seedNamedUser(t, db, "orgmember")
	stranger := seedNamedUser(t, db, "stranger")
	writer := seedNamedUser(t, db, "writer")
	var orgID, personal, orgRepo int64
	if err := db.QueryRow(`INSERT INTO organizations (name) VALUES ('acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	testutil.Exec(t, db, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner'), ($1, $3, 'member')`, orgID, orgOwner, orgMember)
	if err := db.QueryRow(`INSERT INTO repositories (owner_id, owner_name, name) VALUES ($1, 'alice', 'mine') RETURNING id`, alice).Scan(&personal); err != nil {
		t.Fatalf("seed personal repo: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO repositories (owner_name, org_id, name) VALUES ('acme', $1, 'shared') RETURNING id`, orgID).Scan(&orgRepo); err != nil {
		t.Fatalf("seed org repo: %v", err)
	}

	seeded := map[grant]string{
		{personal, alice}:    "owner",
		{personal, stranger}: "owner",
		{personal, writer}:   "writer",
		{orgRepo, orgOwner}:  "owner",
		{orgRepo, orgMember}: "owner",
		{orgRepo, stranger}:  "owner",
	}
	for g, role := range seeded {
		if err := insertPermission(db, g.repo, g.user, role); err != nil {
			t.Fatalf("seed %s row: %v", role, err)
		}
	}
	want := map[grant]string{
		{personal, stranger}: "reader",
		{personal, writer}:   "writer",
		{orgRepo, orgMember}: "reader",
		{orgRepo, stranger}:  "reader",
	}
	migration, err := os.ReadFile(dropOwnerPermissionRoleMigration)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}

	for run := 1; run <= 2; run++ {
		if _, err := db.Exec(string(migration)); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		got := permissionRoles(t, db)
		if len(got) != len(want) {
			t.Errorf("run %d: %d permissions rows, want %d: %v", run, len(got), len(want), got)
		}
		for g, role := range want {
			if got[g] != role {
				t.Errorf("run %d: repo %d user %d has role %q, want %q", run, g.repo, g.user, got[g], role)
			}
		}
	}
}
