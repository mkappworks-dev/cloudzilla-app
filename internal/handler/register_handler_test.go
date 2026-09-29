package handler_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

var dbErrorFragments = []string{"duplicate key", "SQLSTATE", "users_username_key", "users_email_key", "user create", "byte sequence"}

// setAllowRegistration sets allow_registration for the test and restores the
// previous value, or its absence, afterwards.
func setAllowRegistration(t *testing.T, db *sql.DB, value string) {
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
	if err := settings.Set(context.Background(), "allow_registration", value); err != nil {
		t.Fatalf("set allow_registration: %v", err)
	}
}

func enableRegistration(t *testing.T, db *sql.DB) {
	t.Helper()
	setAllowRegistration(t, db, "true")
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

func seedOrgNamed(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	testutil.Exec(t, db, `INSERT INTO organizations (name) VALUES ($1)`, name)
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM organizations WHERE name = $1`, name) })
}

func countUsersWithEmail(t *testing.T, db *sql.DB, email string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM users WHERE lower(email) = lower($1)`, email).Scan(&n); err != nil {
		t.Fatalf("count users: %v", err)
	}
	return n
}

// Users and orgs share one path namespace, compared case-insensitively.
func TestPageRegisterSubmit_OrgNameInOtherCase_SaysUsernameTaken(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	seedOrgNamed(t, db, "acme_"+suffix)
	email := "fresh_" + suffix + "@test.invalid"
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM users WHERE email = $1`, email) })

	body := postRegister(t, db, "Acme_"+suffix, email)

	if !strings.Contains(body, "username is already taken") {
		t.Errorf("want username-taken message; body:\n%s", body)
	}
	if n := countUsersWithEmail(t, db, email); n != 0 {
		t.Errorf("a name taken by an org must not create an account; found %d", n)
	}
}

// The page escapes the rest of the message, so tests match this prefix.
const usernameRuleText = "Usernames can use letters, numbers, - and _"

func TestPageRegisterSubmit_InvalidUsername_ShowsRuleAndCreatesNoAccount(t *testing.T) {
	db := testutil.OpenTestDB(t)
	email := "badname_" + testutil.UniqueSuffix(t) + "@test.invalid"
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM users WHERE email = $1`, email) })
	logs := captureLogs(t)

	body := postRegister(t, db, "../x", email)

	if !strings.Contains(body, usernameRuleText) {
		t.Errorf("want the username rule; body:\n%s", body)
	}
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM users WHERE email = $1`, email).Scan(&n); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n != 0 {
		t.Errorf("an invalid username must not create an account; found %d", n)
	}
	if got := loggedLevel(t, logs, "register: create user failed"); got != "INFO" {
		t.Errorf("an invalid username is a user mistake; want INFO, got %s", got)
	}
}

// captureLogs routes slog to a buffer for the rest of the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func loggedLevel(t *testing.T, logs *bytes.Buffer, msg string) string {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var rec struct{ Level, Msg string }
		if err := json.Unmarshal([]byte(line), &rec); err == nil && rec.Msg == msg {
			return rec.Level
		}
	}
	t.Fatalf("no %q log record in:\n%s", msg, logs.String())
	return ""
}

func TestPageRegisterSubmit_UsernameTaken_LogsAtInfo(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	testutil.SeedUser(t, db, suffix)
	logs := captureLogs(t)

	postRegister(t, db, "testuser_"+suffix, "fresh_"+suffix+"@test.invalid")

	if got := loggedLevel(t, logs, "register: create user failed"); got != "INFO" {
		t.Errorf("a taken username is a user mistake; want INFO, got %s", got)
	}
}

// A NUL byte is rejected by Postgres itself, standing in for any unexpected DB failure.
func TestPageRegisterSubmit_DBFailure_GenericErrorLoggedAtError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	logs := captureLogs(t)

	body := postRegister(t, db, "nul_"+suffix, "nul\x00_"+suffix+"@test.invalid")

	assertNoDBErrorText(t, body)
	if !strings.Contains(body, "Could not create account") {
		t.Errorf("want generic create-account error; body:\n%s", body)
	}
	if got := loggedLevel(t, logs, "register: create user failed"); got != "ERROR" {
		t.Errorf("an unexpected failure must be logged at ERROR, got %s", got)
	}
}
