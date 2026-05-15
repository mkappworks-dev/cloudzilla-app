package handler_test

// Integration tests for repo handler. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// newRepoHandler builds a Handler with services needed by the repo handler.
func newRepoHandler(db *sql.DB) *handler.Handler {
	cfg := &config.Config{
		Auth: config.AuthConfig{
			JWTSecret:  testJWTSecret,
			JWTExpiry:  24 * time.Hour,
			CookieName: testCookieName,
		},
	}
	userSvc := service.NewUserService(store.NewUserStore(db), cfg.Auth)
	repoSvc := service.NewRepoService(store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db), nil, nil, nil, config.GitConfig{})
	svc := &service.Services{
		User:        userSvc,
		Repo:        repoSvc,
		AuditLog:    service.NewAuditService(store.NewAuditLogStore(db)),
		SiteSetting: service.NewSiteSettingService(store.NewSiteSettingStore(db), store.NewUserStore(db)),
	}
	return handler.New(svc, cfg)
}

// repoAPIRouter wraps repo handler methods in a chi.Mux.
func repoAPIRouter(h *handler.Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/api/repos", h.ListRepos)
	r.Post("/api/repos", h.CreateRepo)
	r.Get("/api/repos/{owner}/{repo}", h.GetRepo)
	r.Get("/api/users/{username}/repos", h.ListUserRepos)
	return r
}

// repoAPIRouterWithAuth wraps the repo router in Auth middleware.
func repoAPIRouterWithAuth(h *handler.Handler) http.Handler {
	return middleware.Auth(testJWTSecret, testCookieName, nil, nil, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) })(repoAPIRouter(h))
}

// repoCreateBody serializes repo creation fields into a JSON request buffer.
func repoCreateBody(name, description string, private bool) *bytes.Buffer {
	b, _ := json.Marshal(map[string]any{"name": name, "description": description, "private": private})
	return bytes.NewBuffer(b)
}

// TestCreateRepo_NoAuth_401 verifies that POST /api/repos returns HTTP 401
// when no Authorization header is provided.
func TestCreateRepo_NoAuth_401(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newRepoHandler(db)
	router := repoAPIRouterWithAuth(h)

	req := httptest.NewRequest(http.MethodPost, "/api/repos",
		repoCreateBody("myrepo", "desc", false))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rr.Code)
	}
}

// TestCreateRepo_ValidAuth_201 verifies that an authenticated user can create a
// repository and receives HTTP 201 with the repo ID in the response.
func TestCreateRepo_ValidAuth_201(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix

	h := newRepoHandler(db)
	router := repoAPIRouterWithAuth(h)
	token := makeIssueJWT(t, ownerID, ownerName)

	req := httptest.NewRequest(http.MethodPost, "/api/repos",
		repoCreateBody("newrepo_"+suffix, "Test repo", false))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if id, _ := body["id"].(float64); id == 0 {
		t.Error("created repo must have non-zero ID")
	}
}

// TestGetRepo_ExistingRepo_200 verifies that GET /api/repos/{owner}/{repo} returns
// HTTP 200 with the repository details for a known owner/repo combination.
func TestGetRepo_ExistingRepo_200(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoName := "testrepo_" + suffix
	testutil.SeedRepo(t, db, ownerID, ownerName, suffix)

	h := newRepoHandler(db)
	router := repoAPIRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/repos/"+ownerName+"/"+repoName, nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["name"] != repoName {
		t.Errorf("want repo name %q, got %v", repoName, body["name"])
	}
}

// TestGetRepo_UnknownRepo_404 verifies that GET /api/repos/{owner}/{repo} returns
// HTTP 404 for a repository that does not exist.
func TestGetRepo_UnknownRepo_404(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newRepoHandler(db)
	router := repoAPIRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/repos/nobody/nonexistent", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", rr.Code)
	}
}

// TestListUserRepos_ReturnsRepos verifies that GET /api/users/{username}/repos returns
// HTTP 200 with a JSON array containing the user's repositories.
func TestListUserRepos_ReturnsRepos(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	testutil.SeedRepo(t, db, ownerID, ownerName, suffix)

	h := newRepoHandler(db)
	router := repoAPIRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/users/"+ownerName+"/repos", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	body := strings.TrimSpace(rr.Body.String())
	if !strings.HasPrefix(body, "[") {
		t.Errorf("want JSON array, got: %s", body)
	}
}
