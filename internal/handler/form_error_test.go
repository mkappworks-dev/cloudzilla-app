package handler_test

// Integration tests: form handlers must not echo store or driver errors. All
// tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"database/sql"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// Postgres rejects NUL in text columns (SQLSTATE 22021) and in JSONB (22P05),
// so a NUL in any free-text field forces a driver error past the handler's checks.
const nul = "\x00"

func newFormHandler(t *testing.T, db *sql.DB) *handler.Handler {
	t.Helper()
	cfg := &config.Config{
		Auth: config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName},
		Git:  config.GitConfig{ReposRoot: t.TempDir()},
	}
	return handler.New(service.New(store.New(db), cfg), cfg)
}

func formRouter(h *handler.Handler) http.Handler {
	r := chi.NewRouter()
	r.Get("/{owner}/{repo}/discussions/new", h.PageNewDiscussion)
	r.Post("/{owner}/{repo}/discussions/new", h.PageNewDiscussionSubmit)
	r.Get("/{owner}/{repo}/milestones/new", h.PageNewMilestone)
	r.Post("/{owner}/{repo}/milestones/new", h.PageNewMilestoneSubmit)
	r.Get("/{owner}/{repo}/issues/new", h.PageNewIssue)
	r.Post("/{owner}/{repo}/issues/new", h.PageNewIssueSubmit)
	r.Get("/{owner}/{repo}/pulls/new", h.PageNewPull)
	r.Post("/{owner}/{repo}/pulls/new", h.PageNewPullSubmit)
	r.Post("/admin/sso", h.SaveSSOConfig)
	r.Post("/setup", h.PageSetupSubmit)
	r.Post("/register", h.PageRegisterSubmit)
	r.Post("/invite/{token}", h.PageInviteSubmit)
	return middleware.OptionalAuth(testJWTSecret, testCookieName, nil, nil)(r)
}

func submitForm(t *testing.T, h *handler.Handler, path, token string, form url.Values) string {
	t.Helper()
	return postForm(t, formRouter(h), token, path, form).Body.String()
}

func assertNoRawDBError(t *testing.T, body string) {
	t.Helper()
	for _, leak := range []string{"SQLSTATE", "duplicate key", "violates"} {
		if strings.Contains(body, leak) {
			t.Errorf("body must not contain DB error text %q", leak)
		}
	}
}

func assertContains(t *testing.T, body, want string) {
	t.Helper()
	if !strings.Contains(body, want) {
		t.Errorf("want %q in body:\n%s", want, body)
	}
}

// seedFormRepo returns the repo path and a token for its owner.
func seedFormRepo(t *testing.T, db *sql.DB) (repoPath, token string) {
	t.Helper()
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	return "/" + ownerName + "/testrepo_" + suffix, makeIssueJWT(t, ownerID, ownerName)
}

func TestPageNewDiscussionSubmit_UnknownCategory_GenericError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repoPath, token := seedFormRepo(t, db)

	body := submitForm(t, newFormHandler(t, db), repoPath+"/discussions/new", token, url.Values{
		"title": {"Hello"}, "body": {"b"}, "category_id": {"9223372036854775807"},
	})

	assertNoRawDBError(t, body)
	assertContains(t, body, "Could not create the discussion")
}

func TestPageNewDiscussionSubmit_TitleTooLong_SaysSo(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repoPath, token := seedFormRepo(t, db)

	body := submitForm(t, newFormHandler(t, db), repoPath+"/discussions/new", token, url.Values{
		"title": {strings.Repeat("x", service.MaxTitleLen+1)}, "category_id": {"1"},
	})

	assertContains(t, body, "Title is too long")
}

func TestPageNewMilestoneSubmit_StoreError_GenericError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repoPath, token := seedFormRepo(t, db)

	body := submitForm(t, newFormHandler(t, db), repoPath+"/milestones/new", token, url.Values{
		"title": {"v1" + nul},
	})

	assertNoRawDBError(t, body)
	assertContains(t, body, "Could not create the milestone")
}

func TestPageNewIssueSubmit_StoreError_GenericError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repoPath, token := seedFormRepo(t, db)

	body := submitForm(t, newFormHandler(t, db), repoPath+"/issues/new", token, url.Values{
		"title": {"Bug"}, "body": {"b" + nul},
	})

	assertNoRawDBError(t, body)
	assertContains(t, body, "Could not create the issue")
}

func TestPageNewIssueSubmit_TitleTooLong_SaysSo(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repoPath, token := seedFormRepo(t, db)

	body := submitForm(t, newFormHandler(t, db), repoPath+"/issues/new", token, url.Values{
		"title": {strings.Repeat("x", service.MaxTitleLen+1)},
	})

	assertContains(t, body, "Title is too long")
}

func TestPageNewPullSubmit_StoreError_GenericError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repoPath, token := seedFormRepo(t, db)

	body := submitForm(t, newFormHandler(t, db), repoPath+"/pulls/new", token, url.Values{
		"title": {"Change"}, "body": {"b" + nul}, "head_branch": {"feature"}, "base_branch": {"main"},
	})

	assertNoRawDBError(t, body)
	assertContains(t, body, "Could not create the pull request")
}

func TestPageNewPullSubmit_TitleTooLong_SaysSo(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repoPath, token := seedFormRepo(t, db)

	body := submitForm(t, newFormHandler(t, db), repoPath+"/pulls/new", token, url.Values{
		"title": {strings.Repeat("x", service.MaxTitleLen+1)}, "head_branch": {"feature"}, "base_branch": {"main"},
	})

	assertContains(t, body, "Title is too long")
}

func TestSaveSSOConfig_StoreError_GenericError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	adminID := testutil.SeedSuperadmin(t, db, suffix)

	body := submitForm(t, newFormHandler(t, db), "/admin/sso", makeSuperadminJWT(t, adminID, "testadmin_"+suffix), url.Values{
		"provider": {"ldap"}, "ldap_host": {"ldap.test.invalid" + nul},
	})

	assertNoRawDBError(t, body)
	assertContains(t, body, "Could not save the SSO configuration")
}

// openSchemalessDB connects to the test database with a search_path that has
// no tables, so every query fails. Setup then looks incomplete (the user count
// fails) and site settings fall back to their defaults, without touching
// shared rows.
func openSchemalessDB(t *testing.T) *sql.DB {
	t.Helper()
	testutil.OpenTestDB(t)
	dsn, err := url.Parse(os.Getenv("TEST_DATABASE_DSN"))
	if err != nil || !strings.HasPrefix(dsn.Scheme, "postgres") {
		t.Skip("needs a URL-form TEST_DATABASE_DSN")
	}
	q := dsn.Query()
	q.Set("search_path", "cz_test_no_such_schema")
	dsn.RawQuery = q.Encode()
	db, err := sql.Open("pgx", dsn.String())
	if err != nil {
		t.Fatalf("open schemaless db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestPageSetupSubmit_StoreError_GenericError(t *testing.T) {
	db := openSchemalessDB(t)

	body := submitForm(t, newFormHandler(t, db), "/setup", "", url.Values{
		"username": {"admin"}, "email": {"admin@test.invalid"}, "password": {"password123"},
	})

	assertNoRawDBError(t, body)
	assertContains(t, body, "Could not create the admin account")
}
