package handler_test

// Integration tests for pull request handler. All tests require TEST_DATABASE_DSN and skip otherwise.

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

// newPullHandler builds a Handler with the full service set needed by the pull handler.
func newPullHandler(db *sql.DB) *handler.Handler {
	cfg := &config.Config{
		Auth: config.AuthConfig{
			JWTSecret:  testJWTSecret,
			JWTExpiry:  24 * time.Hour,
			CookieName: testCookieName,
		},
	}
	userSvc := service.NewUserService(store.NewUserStore(db), cfg.Auth)
	repoSvc := service.NewRepoService(store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db), nil, nil, config.GitConfig{})
	emailSvc := service.NewEmailService(config.SMTPConfig{})
	notifSvc := service.NewNotificationService(store.NewNotificationStore(db), store.NewWatchStore(db), emailSvc, userSvc)
	svc := &service.Services{
		User:         userSvc,
		Repo:         repoSvc,
		Pull:         service.NewPullService(store.NewPullStore(db), store.NewRepoStore(db), repoSvc),
		Webhook:      service.NewWebhookService(store.NewWebhookStore(db)),
		Event:        service.NewEventService(store.NewEventStore(db), store.NewUserStore(db), store.NewRepoStore(db)),
		Notification: notifSvc,
		Code:         service.NewCodeService(config.GitConfig{}),
		Assignee:     service.NewAssigneeService(store.NewAssigneeStore(db), store.NewRepoStore(db), store.NewIssueStore(db), store.NewPullStore(db), store.NewUserStore(db)),
		AuditLog:     service.NewAuditService(store.NewAuditLogStore(db)),
		SiteSetting:  service.NewSiteSettingService(store.NewSiteSettingStore(db), store.NewUserStore(db)),
	}
	return handler.New(svc, cfg)
}

// pullRouter wraps pull handler methods in a chi.Mux so URL parameters populate.
func pullRouter(h *handler.Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/api/repos/{owner}/{repo}/pulls", h.ListPulls)
	r.Post("/api/repos/{owner}/{repo}/pulls", h.CreatePull)
	r.Patch("/api/repos/{owner}/{repo}/pulls/{number}", h.UpdatePull)
	return r
}

// pullRouterWithAuth wraps the pull router in the Auth middleware.
func pullRouterWithAuth(h *handler.Handler) http.Handler {
	return middleware.Auth(testJWTSecret, testCookieName, nil, nil, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) })(pullRouter(h))
}

// createPullBody serializes pull request fields into a JSON request body.
func createPullBody(title, head, base string, draft bool) *bytes.Buffer {
	b, _ := json.Marshal(map[string]any{"title": title, "head_branch": head, "base_branch": base, "is_draft": draft})
	return bytes.NewBuffer(b)
}

// TestCreatePull_NoAuth_401 verifies that POST /api/repos/{owner}/{repo}/pulls returns
// HTTP 401 when no Authorization header is present.
func TestCreatePull_NoAuth_401(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newPullHandler(db)
	router := pullRouterWithAuth(h)

	req := httptest.NewRequest(http.MethodPost, "/api/repos/owner/repo/pulls",
		createPullBody("My PR", "feature", "main", false))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rr.Code)
	}
}

// TestCreatePull_ValidAuth_201 verifies that an authenticated user can create a pull
// request and receives HTTP 201 with the created PR (non-zero ID, open state).
func TestCreatePull_ValidAuth_201(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoName := "testrepo_" + suffix
	testutil.SeedRepo(t, db, ownerID, ownerName, suffix)

	h := newPullHandler(db)
	router := pullRouterWithAuth(h)
	token := makeIssueJWT(t, ownerID, ownerName)

	req := httptest.NewRequest(http.MethodPost,
		"/api/repos/"+ownerName+"/"+repoName+"/pulls",
		createPullBody("Test PR", "feature", "main", false))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if id, _ := body["id"].(float64); id == 0 {
		t.Error("created PR must have non-zero ID")
	}
	if body["state"] != "open" {
		t.Errorf("new PR must be open, got %v", body["state"])
	}
}

// TestListPulls_NoAuth_200 verifies that listing pull requests requires no authentication
// and returns HTTP 200 with a JSON array.
func TestListPulls_NoAuth_200(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoName := "testrepo_" + suffix
	testutil.SeedRepo(t, db, ownerID, ownerName, suffix)

	h := newPullHandler(db)
	router := pullRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/repos/"+ownerName+"/"+repoName+"/pulls", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if body := strings.TrimSpace(rr.Body.String()); !strings.HasPrefix(body, "[") {
		t.Errorf("want JSON array, got: %s", body)
	}
}

// TestUpdatePull_NoAuth_401 verifies that PATCH /api/repos/{owner}/{repo}/pulls/{number}
// returns HTTP 401 when no Authorization header is present.
func TestUpdatePull_NoAuth_401(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newPullHandler(db)
	router := pullRouterWithAuth(h)

	req := httptest.NewRequest(http.MethodPatch, "/api/repos/owner/repo/pulls/1",
		strings.NewReader(`{"state":"closed"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rr.Code)
	}
}
