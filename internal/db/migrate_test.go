package db_test

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const caseInsensitiveEmailMigration = "migrations/074_users_email_case_insensitive.sql"

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
