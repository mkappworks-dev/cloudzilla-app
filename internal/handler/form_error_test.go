package handler_test

// Integration tests: form handlers must not echo store or driver errors. All
// tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"database/sql"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// Postgres rejects NUL in text columns (SQLSTATE 22021) and in JSONB (22P05),
// so a NUL in any free-text field forces a driver error past the handler's checks.
const nul = "\x00"

func assertNoRawDBError(t *testing.T, body string) {
	t.Helper()
	for _, leak := range []string{"SQLSTATE", "duplicate key", "violates"} {
		if strings.Contains(body, leak) {
			t.Errorf("body must not contain DB error text %q", leak)
		}
	}
}

func TestPageNewDiscussionSubmit_UnknownCategory_AsksForCategory(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := seedOwnedRepo(t, db, false)

	body := submitForm(t, newPageHandler(t, db), repo.path+"/discussions/new", repo.owner.token, url.Values{
		"title": {"Hello"}, "body": {"b"}, "category_id": {"9223372036854775807"},
	})

	assertNoRawDBError(t, body)
	assertContains(t, body, "Pick a category for your discussion")
}

func TestPageNewDiscussionSubmit_StoreError_GenericError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := seedOwnedRepo(t, db, false)
	var categoryID string
	if err := db.QueryRowContext(context.Background(), `SELECT id FROM discussion_categories ORDER BY id LIMIT 1`).Scan(&categoryID); err != nil {
		t.Fatalf("load a discussion category: %v", err)
	}

	body := submitForm(t, newPageHandler(t, db), repo.path+"/discussions/new", repo.owner.token, url.Values{
		"title": {"Hello"}, "body": {"b" + nul}, "category_id": {categoryID},
	})

	assertNoRawDBError(t, body)
	assertContains(t, body, "Could not create the discussion")
}

func TestPageNewDiscussionSubmit_TitleTooLong_SaysSo(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := seedOwnedRepo(t, db, false)

	body := submitForm(t, newPageHandler(t, db), repo.path+"/discussions/new", repo.owner.token, url.Values{
		"title": {strings.Repeat("x", service.MaxTitleLen+1)}, "category_id": {"1"},
	})

	assertContains(t, body, "Title is too long")
}

func TestPageNewMilestoneSubmit_StoreError_GenericError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := seedOwnedRepo(t, db, false)

	body := submitForm(t, newPageHandler(t, db), repo.path+"/milestones/new", repo.owner.token, url.Values{
		"title": {"v1" + nul},
	})

	assertNoRawDBError(t, body)
	assertContains(t, body, "Could not create the milestone")
}

// Milestone numbers are MAX(number)+1, so a concurrent create can take the
// number this request computed and fail its insert with a unique violation
// (SQLSTATE 23505). Holding that number in an open transaction makes the race
// deterministic.
func TestPageNewMilestoneSubmit_NumberTaken_GenericError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := seedOwnedRepo(t, db, false)
	h := newPageHandler(t, db)
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	if _, err := tx.Exec(`INSERT INTO milestones (repo_id, number, title) VALUES ($1, 1, 'held')`, repo.id); err != nil {
		t.Fatalf("hold milestone number: %v", err)
	}

	bodyCh := make(chan string, 1)
	go func() {
		bodyCh <- submitForm(t, h, repo.path+"/milestones/new", repo.owner.token, url.Values{"title": {"v1"}})
	}()
	waitForLockWait(t, db, "%INSERT INTO milestones%")
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	body := <-bodyCh

	assertNoRawDBError(t, body)
	assertContains(t, body, "Could not create the milestone")
}

// waitForLockWait returns once another session running a query that matches
// pattern is blocked on a lock.
func waitForLockWait(t *testing.T, db *sql.DB, pattern string) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		var waiting int
		err := db.QueryRowContext(context.Background(),
			`SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND query LIKE $1`, pattern,
		).Scan(&waiting)
		if err != nil {
			t.Fatalf("poll pg_stat_activity: %v", err)
		}
		if waiting > 0 {
			return
		}
	}
	t.Fatalf("no query matching %q blocked on a lock within 5s", pattern)
}

func TestPageNewIssueSubmit_StoreError_GenericError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := seedOwnedRepo(t, db, false)

	body := submitForm(t, newPageHandler(t, db), repo.path+"/issues/new", repo.owner.token, url.Values{
		"title": {"Bug"}, "body": {"b" + nul},
	})

	assertNoRawDBError(t, body)
	assertContains(t, body, "Could not create the issue")
}

// The private checkbox is shown only to writers, so a non-writer reaches this
// by crafting the POST or losing write access mid-form.
func TestPageNewIssueSubmit_PrivateIssueByNonWriter_SaysSo(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := seedOwnedRepo(t, db, false)

	body := submitForm(t, newPageHandler(t, db), repo.path+"/issues/new", seedSignedInUser(t, db).token, url.Values{
		"title": {"Bug"}, "visibility": {"private"},
	})

	assertContains(t, body, "Only collaborators with write access can create private issues")
}

func TestPageNewIssueSubmit_TitleTooLong_SaysSo(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := seedOwnedRepo(t, db, false)

	body := submitForm(t, newPageHandler(t, db), repo.path+"/issues/new", repo.owner.token, url.Values{
		"title": {strings.Repeat("x", service.MaxTitleLen+1)},
	})

	assertContains(t, body, "Title is too long")
}

func TestPageNewPullSubmit_StoreError_GenericError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := seedOwnedRepo(t, db, false)

	body := submitForm(t, newPageHandler(t, db), repo.path+"/pulls/new", repo.owner.token, url.Values{
		"title": {"Change"}, "body": {"b" + nul}, "head_branch": {"feature"}, "base_branch": {"main"},
	})

	assertNoRawDBError(t, body)
	assertContains(t, body, "Could not create the pull request")
}

func TestPageNewPullSubmit_TitleTooLong_SaysSo(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := seedOwnedRepo(t, db, false)

	body := submitForm(t, newPageHandler(t, db), repo.path+"/pulls/new", repo.owner.token, url.Values{
		"title": {strings.Repeat("x", service.MaxTitleLen+1)}, "head_branch": {"feature"}, "base_branch": {"main"},
	})

	assertContains(t, body, "Title is too long")
}

func TestSaveSSOConfig_StoreError_GenericError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	adminID := testutil.SeedSuperadmin(t, db, suffix)
	testutil.SetPassword(t, db, adminID, "admin-password")

	body := submitForm(t, newPageHandler(t, db), "/admin/sso", makeSuperadminJWT(t, adminID, "testadmin_"+suffix), url.Values{
		"provider": {"ldap"}, "ldap_host": {"ldap.test.invalid" + nul}, "password": {"admin-password"},
	})

	assertNoRawDBError(t, body)
	assertContains(t, body, "Could not save the SSO configuration")
}

func TestPageSetupSubmit_StoreError_GenericError(t *testing.T) {
	db := openSchemalessDB(t)

	body := submitForm(t, newPageHandler(t, db), "/setup", "", url.Values{
		"username": {"siteadmin"}, "email": {"admin@test.invalid"}, "password": {"password123"},
	})

	assertNoRawDBError(t, body)
	assertContains(t, body, "Could not create the admin account")
}
