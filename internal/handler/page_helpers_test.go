package handler_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
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

func newPageHandler(t *testing.T, db *sql.DB) *handler.Handler {
	t.Helper()
	cfg := &config.Config{
		Auth: config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName},
		Git:  config.GitConfig{ReposRoot: t.TempDir()},
	}
	return handler.New(service.New(store.New(db), cfg), cfg)
}

func pageRouter(h *handler.Handler) http.Handler {
	r := chi.NewRouter()
	r.Get("/{owner}/{repo}/discussions/new", h.PageNewDiscussion)
	r.Post("/{owner}/{repo}/discussions/new", h.PageNewDiscussionSubmit)
	r.Get("/{owner}/{repo}/milestones/new", h.PageNewMilestone)
	r.Post("/{owner}/{repo}/milestones/new", h.PageNewMilestoneSubmit)
	r.Get("/{owner}/{repo}/issues/new", h.PageNewIssue)
	r.Post("/{owner}/{repo}/issues/new", h.PageNewIssueSubmit)
	r.Get("/{owner}/{repo}/pulls/new", h.PageNewPull)
	r.Post("/{owner}/{repo}/pulls/new", h.PageNewPullSubmit)
	r.Post("/{owner}/{repo}/milestones/{number}", h.PageMilestoneDetailAction)
	r.Get("/{owner}/{repo}/releases/new", h.PageReleaseNew)
	r.Get("/{owner}/{repo}/new/{ref}", h.PageNewFile)
	r.Post("/{owner}/{repo}/new/{ref}", h.SubmitNewFile)
	r.Get("/{owner}/{repo}/settings", h.PageRepoSettings)
	r.Post("/{owner}/{repo}/settings/general", h.UpdateRepoGeneral)
	r.Post("/{owner}/{repo}/settings/features", h.UpdateRepoFeatures)
	r.Post("/{owner}/{repo}/settings/visibility", h.UpdateRepoVisibility)
	r.Get("/{owner}/{repo}/wiki/new", h.PageWikiNew)
	r.Get("/{owner}/{repo}/wiki/{slug}/edit", h.PageWikiEdit)
	r.Post("/admin/sso", h.SaveSSOConfig)
	r.Post("/setup", h.PageSetupSubmit)
	r.Post("/register", h.PageRegisterSubmit)
	r.Post("/invite/{token}", h.PageInviteSubmit)
	return middleware.OptionalAuth(testJWTSecret, testCookieName, nil, nil)(r)
}

func submitForm(t *testing.T, h *handler.Handler, path, token string, form url.Values) string {
	t.Helper()
	return postForm(t, pageRouter(h), token, path, form).Body.String()
}

// POSTs send an empty title: the validation error that re-renders a form with
// the repo's data.
var emptyTitleForm = url.Values{"title": {""}, "head_branch": {"feature"}, "base_branch": {"main"}}

func requestPage(t *testing.T, db *sql.DB, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	router := pageRouter(newPageHandler(t, db))
	if method == http.MethodPost {
		return postForm(t, router, token, path, emptyTitleForm)
	}
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

func assertContains(t *testing.T, body, want string) {
	t.Helper()
	if !strings.Contains(body, want) {
		t.Errorf("want %q in body:\n%s", want, body)
	}
}

type signedInUser struct {
	id          int64
	name, token string
}

func seedSignedInUser(t *testing.T, db *sql.DB) signedInUser {
	t.Helper()
	suffix := testutil.UniqueSuffix(t)
	id := testutil.SeedUser(t, db, suffix)
	name := "testuser_" + suffix // testutil.SeedUser's naming
	return signedInUser{id: id, name: name, token: makeIssueJWT(t, id, name)}
}

type seededRepo struct {
	id         int64
	path, name string
	owner      signedInUser
}

func seedOwnedRepo(t *testing.T, db *sql.DB, private bool) seededRepo {
	t.Helper()
	owner := seedSignedInUser(t, db)
	suffix := testutil.UniqueSuffix(t)
	repoID := testutil.SeedRepo(t, db, owner.id, owner.name, suffix)
	if private {
		testutil.Exec(t, db, `UPDATE repositories SET private = true WHERE id = $1`, repoID)
	}
	name := "testrepo_" + suffix // testutil.SeedRepo's naming
	return seededRepo{id: repoID, path: "/" + owner.name + "/" + name, name: name, owner: owner}
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
