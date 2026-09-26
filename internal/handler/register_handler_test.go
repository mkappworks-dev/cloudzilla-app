package handler_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

var dbErrorFragments = []string{"duplicate key", "SQLSTATE", "users_username_key", "users_email_key", "user create"}

// enableRegistration turns allow_registration on for the test and restores the
// previous value, or its absence, afterwards.
func enableRegistration(t *testing.T, db *sql.DB) {
	t.Helper()
	settings := store.NewSiteSettingStore(db)
	prev, err := settings.Get(context.Background(), "allow_registration")
	switch {
	case errors.Is(err, sql.ErrNoRows):
		t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM site_settings WHERE key = 'allow_registration'`) })
	case err != nil:
		t.Fatalf("read allow_registration: %v", err)
	default:
		t.Cleanup(func() {
			testutil.Exec(t, db, `UPDATE site_settings SET value = $1 WHERE key = 'allow_registration'`, prev)
		})
	}
	if err := settings.Set(context.Background(), "allow_registration", "true"); err != nil {
		t.Fatalf("enable registration: %v", err)
	}
}

func postRegister(t *testing.T, db *sql.DB, username, email string) string {
	t.Helper()
	enableRegistration(t, db)
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
