package db_test

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const caseInsensitiveEmailMigration = "migrations/082_users_email_case_insensitive.sql"

func TestEmailCaseMigration_CaseVariantEmails_FailsNamingUserIDs(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	sql, err := os.ReadFile(caseInsensitiveEmailMigration)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	testutil.Exec(t, db, `DROP INDEX users_email_lower_key`)
	var ids []int64
	for _, email := range []string{"dup@test.invalid", "Dup@Test.Invalid"} {
		var id int64
		if err := db.QueryRow(
			`INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x') RETURNING id`,
			"dup_"+strconv.Itoa(len(ids)), email,
		).Scan(&id); err != nil {
			t.Fatalf("seed user: %v", err)
		}
		ids = append(ids, id)
	}

	_, err = db.Exec(string(sql))

	if err == nil {
		t.Fatal("migration must fail while emails differ only by case")
	}
	for _, id := range ids {
		if !strings.Contains(err.Error(), strconv.FormatInt(id, 10)) {
			t.Errorf("error must name user %d: %v", id, err)
		}
	}
	if strings.Contains(strings.ToLower(err.Error()), "dup@test.invalid") {
		t.Errorf("error must not include the email addresses: %v", err)
	}
}

func TestSignupTokensMigration_OneRowPerEmailIgnoringCase(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	testutil.Exec(t, db, `INSERT INTO signup_tokens (token_hash, email, expires_at) VALUES ('h1', 'a@test.invalid', NOW())`)

	_, err := db.Exec(`INSERT INTO signup_tokens (token_hash, email, expires_at) VALUES ('h2', 'A@Test.Invalid', NOW())`)

	if err == nil {
		t.Error("want a unique violation for an email that differs only by case")
	}
}

func TestOrganizationsNameLowerIndex_ServesTheOwnerNameGuard(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}

	rows, err := tx.Query(`EXPLAIN SELECT 1 FROM organizations WHERE lower(name) = lower($1)`, "acme")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line + "\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(plan.String(), "idx_organizations_name_lower") {
		t.Errorf("the guard's org lookup must be able to use idx_organizations_name_lower; plan:\n%s", plan.String())
	}
}

const ghostUserMigration = "migrations/089_ghost_user.sql"

func TestGhostUserMigration_SeedsTheOnlyUserAndItCannotSignIn(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("users on a fresh install = %d, %v; want just the ghost", n, err)
	}
	var username, passwordHash, oauthProvider string
	var ssoProvider *string
	var emailNotifications, superadmin bool
	if err := db.QueryRow(
		`SELECT username, password_hash, oauth_provider, sso_provider, email_notifications, is_superadmin
		 FROM users WHERE id = ghost_user_id()`,
	).Scan(&username, &passwordHash, &oauthProvider, &ssoProvider, &emailNotifications, &superadmin); err != nil {
		t.Fatalf("load ghost: %v", err)
	}
	if username != "ghost" {
		t.Errorf("username = %q, want ghost", username)
	}
	if passwordHash != "" || oauthProvider != "" || ssoProvider != nil || superadmin {
		t.Errorf("ghost has a way in: password %q, oauth %q, sso %v, superadmin %v", passwordHash, oauthProvider, ssoProvider, superadmin)
	}
	if emailNotifications {
		t.Error("ghost gets notification email")
	}
}

// Run again on a fresh schema with the migration's objects removed, as on an
// install from before it where someone already holds the name.
func TestGhostUserMigration_TakesAFreeNameWhenGhostIsHeld(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	sql, err := os.ReadFile(ghostUserMigration)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	testutil.Exec(t, db, `DROP TRIGGER users_keep_ghost ON users`)
	testutil.Exec(t, db, `DROP INDEX pull_reviews_pull_id_author_id_key`)
	testutil.Exec(t, db, `ALTER TABLE pull_reviews ADD CONSTRAINT pull_reviews_pull_id_author_id_key UNIQUE (pull_id, author_id)`)
	testutil.Exec(t, db, `DELETE FROM users`)
	testutil.Exec(t, db, `DROP FUNCTION users_keep_ghost(), ghost_user_id()`)
	testutil.Exec(t, db, `INSERT INTO users (username, email, password_hash) VALUES ('Ghost', 'held@test.invalid', 'x')`)

	if _, err := db.Exec(string(sql)); err != nil {
		t.Fatalf("migration: %v", err)
	}

	var username string
	if err := db.QueryRow(`SELECT username FROM users WHERE id = ghost_user_id()`).Scan(&username); err != nil {
		t.Fatalf("load ghost: %v", err)
	}
	if username != "ghost2" {
		t.Errorf("ghost username = %q, want ghost2", username)
	}
}

func TestGhostUser_CannotBeDeleted(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)

	if _, err := db.Exec(`DELETE FROM users WHERE id = ghost_user_id()`); err == nil {
		t.Fatal("deleted the ghost; content reassigned to it would then block every account delete")
	}
}
