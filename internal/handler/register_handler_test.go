package handler_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

var dbErrorFragments = []string{"duplicate key", "SQLSTATE", "users_username_key", "users_email_key", "user create"}

func postRegister(t *testing.T, db *sql.DB, username, email string) string {
	t.Helper()
	if err := store.NewSiteSettingStore(db).Set(context.Background(), "allow_registration", "true"); err != nil {
		t.Fatalf("enable registration: %v", err)
	}
	form := url.Values{"username": {username}, "email": {email}, "password": {"password123"}}
	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	newAuthHandler(db).PageRegisterSubmit(rr, req)
	return rr.Body.String()
}

func assertNoDBErrorText(t *testing.T, body string) {
	t.Helper()
	for _, leak := range dbErrorFragments {
		if strings.Contains(body, leak) {
			t.Errorf("body must not contain DB error text %q", leak)
		}
	}
}

func TestPageRegisterSubmit_EmailTaken_GenericError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	testutil.SeedUser(t, db, suffix)

	body := postRegister(t, db, "fresh_"+suffix, "testuser_"+suffix+"@test.invalid")

	assertNoDBErrorText(t, body)
	if !strings.Contains(body, "Could not create account") {
		t.Errorf("want generic create-account error; body:\n%s", body)
	}
}

func TestPageRegisterSubmit_UsernameTaken_SaysUsernameTaken(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	testutil.SeedUser(t, db, suffix)

	body := postRegister(t, db, "testuser_"+suffix, "fresh_"+suffix+"@test.invalid")

	assertNoDBErrorText(t, body)
	if !strings.Contains(body, "username is already taken") {
		t.Errorf("want username-taken message; body:\n%s", body)
	}
}
