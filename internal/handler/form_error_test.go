package handler_test

// Integration tests: form handlers must not echo store or driver errors. All
// tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"net/url"
	"strings"
	"testing"

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

func TestPageNewDiscussionSubmit_UnknownCategory_GenericError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := seedOwnedRepo(t, db, false)

	body := submitForm(t, newPageHandler(t, db), repo.path+"/discussions/new", repo.owner.token, url.Values{
		"title": {"Hello"}, "body": {"b"}, "category_id": {"9223372036854775807"},
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

func TestPageNewIssueSubmit_StoreError_GenericError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := seedOwnedRepo(t, db, false)

	body := submitForm(t, newPageHandler(t, db), repo.path+"/issues/new", repo.owner.token, url.Values{
		"title": {"Bug"}, "body": {"b" + nul},
	})

	assertNoRawDBError(t, body)
	assertContains(t, body, "Could not create the issue")
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

	body := submitForm(t, newPageHandler(t, db), "/admin/sso", makeSuperadminJWT(t, adminID, "testadmin_"+suffix), url.Values{
		"provider": {"ldap"}, "ldap_host": {"ldap.test.invalid" + nul},
	})

	assertNoRawDBError(t, body)
	assertContains(t, body, "Could not save the SSO configuration")
}

func TestPageSetupSubmit_StoreError_GenericError(t *testing.T) {
	db := openSchemalessDB(t)

	body := submitForm(t, newPageHandler(t, db), "/setup", "", url.Values{
		"username": {"admin"}, "email": {"admin@test.invalid"}, "password": {"password123"},
	})

	assertNoRawDBError(t, body)
	assertContains(t, body, "Could not create the admin account")
}
