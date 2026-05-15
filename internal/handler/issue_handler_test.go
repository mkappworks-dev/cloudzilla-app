package handler_test

// Integration tests for issue handler. All tests require TEST_DATABASE_DSN and skip otherwise.

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
	"github.com/golang-jwt/jwt/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// newIssueHandler builds a Handler with the full set of services required by the
// issue handler: Issue, Repo, Webhook, Event, Notification, and SiteSetting.
func newIssueHandler(db *sql.DB) *handler.Handler {
	cfg := &config.Config{
		Auth: config.AuthConfig{
			JWTSecret:  testJWTSecret,
			JWTExpiry:  24 * time.Hour,
			CookieName: testCookieName,
		},
	}
	stores := &store.Stores{
		User:         store.NewUserStore(db),
		Repo:         store.NewRepoStore(db),
		Issue:        store.NewIssueStore(db),
		Webhook:      store.NewWebhookStore(db),
		Notification: store.NewNotificationStore(db),
		Watch:        store.NewWatchStore(db),
		Event:        store.NewEventStore(db),
		Org:          store.NewOrgStore(db),
		SiteSetting:  store.NewSiteSettingStore(db),
		AuditLog:     store.NewAuditLogStore(db),
	}
	userSvc := service.NewUserService(stores.User, cfg.Auth)
	repoSvc := service.NewRepoService(stores.Repo, stores.User, stores.Org, nil, nil, nil, config.GitConfig{})
	emailSvc := service.NewEmailService(config.SMTPConfig{})
	svc := &service.Services{
		User:         userSvc,
		Repo:         repoSvc,
		Issue:        service.NewIssueService(stores.Issue, stores.Repo, repoSvc),
		Webhook:      service.NewWebhookService(stores.Webhook),
		Notification: service.NewNotificationService(stores.Notification, stores.Watch, emailSvc, userSvc),
		Event:        service.NewEventService(stores.Event, stores.User, stores.Repo),
		SiteSetting:  service.NewSiteSettingService(stores.SiteSetting, stores.User),
		AuditLog:     service.NewAuditService(stores.AuditLog),
	}
	return handler.New(svc, cfg)
}

// makeIssueJWT creates a signed HS256 JWT for the given user, used to authenticate
// issue handler requests by injecting claims via the Auth middleware.
func makeIssueJWT(t *testing.T, userID int64, username string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"sub":      float64(userID),
		"username": username,
		"exp":      float64(time.Now().Add(time.Hour).Unix()),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("makeIssueJWT: %v", err)
	}
	return s
}

// issueRouter wraps individual issue handler methods in a chi.Mux so that chi URL
// parameters ({owner}, {repo}, {number}) are populated correctly during tests.
func issueRouter(h *handler.Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/api/repos/{owner}/{repo}/issues", h.ListIssues)
	r.Post("/api/repos/{owner}/{repo}/issues", h.CreateIssue)
	r.Patch("/api/repos/{owner}/{repo}/issues/{number}", h.UpdateIssue)
	return r
}

// issueRouterWithAuth wraps the issue router in the Auth middleware so that a valid
// JWT in the Authorization header is decoded and claims are set in the request context.
func issueRouterWithAuth(h *handler.Handler) http.Handler {
	r := issueRouter(h)
	authMW := middleware.Auth(testJWTSecret, testCookieName, nil, nil, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) })
	return authMW(r)
}

// createIssueBody serializes title, body, and visibility into a JSON request body.
func createIssueBody(title, body, visibility string) *bytes.Buffer {
	b, _ := json.Marshal(map[string]string{"title": title, "body": body, "visibility": visibility})
	return bytes.NewBuffer(b)
}

// TestCreateIssue_NoAuth_401 verifies that POST /api/repos/{owner}/{repo}/issues
// returns HTTP 401 when no Authorization header is provided.
func TestCreateIssue_NoAuth_401(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newIssueHandler(db)
	router := issueRouterWithAuth(h)

	req := httptest.NewRequest(http.MethodPost, "/api/repos/owner/repo/issues",
		createIssueBody("title", "body", "public"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rr.Code)
	}
}

// TestCreateIssue_ValidAuth_201 verifies that an authenticated user can create an issue
// and receives HTTP 201 with the created issue body (non-zero ID, open state).
func TestCreateIssue_ValidAuth_201(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoName := "testrepo_" + suffix
	testutil.SeedRepo(t, db, ownerID, ownerName, suffix)

	h := newIssueHandler(db)
	router := issueRouterWithAuth(h)

	token := makeIssueJWT(t, ownerID, ownerName)
	req := httptest.NewRequest(http.MethodPost,
		"/api/repos/"+ownerName+"/"+repoName+"/issues",
		createIssueBody("Test Issue", "Issue body", "public"))
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
		t.Error("created issue must have non-zero ID")
	}
	if body["state"] != "open" {
		t.Errorf("want state open, got %v", body["state"])
	}
}

// TestListIssues_PublicRepo_NoAuth_200 verifies that listing issues on a public
// repository requires no authentication and returns HTTP 200 with a JSON array.
func TestListIssues_PublicRepo_NoAuth_200(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoName := "testrepo_" + suffix
	testutil.SeedRepo(t, db, ownerID, ownerName, suffix)

	h := newIssueHandler(db)
	// ListIssues does not require auth — use the router without auth middleware.
	router := issueRouter(h)

	req := httptest.NewRequest(http.MethodGet,
		"/api/repos/"+ownerName+"/"+repoName+"/issues", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	// Response must be a JSON array (possibly empty).
	body := rr.Body.String()
	if !strings.HasPrefix(strings.TrimSpace(body), "[") {
		t.Errorf("want JSON array, got: %s", body)
	}
}

// TestUpdateIssue_NoAuth_401 verifies that PATCH /api/repos/{owner}/{repo}/issues/{number}
// returns HTTP 401 when no Authorization header is present.
func TestUpdateIssue_NoAuth_401(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newIssueHandler(db)
	router := issueRouterWithAuth(h)

	req := httptest.NewRequest(http.MethodPatch, "/api/repos/owner/repo/issues/1",
		strings.NewReader(`{"state":"closed"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rr.Code)
	}
}

// TestUpdateIssue_InvalidState_400 verifies that an authenticated request with an
// unrecognized state value returns HTTP 400 (only "open" and "closed" are valid).
func TestUpdateIssue_InvalidState_400(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoName := "testrepo_" + suffix
	testutil.SeedRepo(t, db, ownerID, ownerName, suffix)

	h := newIssueHandler(db)
	router := issueRouterWithAuth(h)

	token := makeIssueJWT(t, ownerID, ownerName)
	req := httptest.NewRequest(http.MethodPatch,
		"/api/repos/"+ownerName+"/"+repoName+"/issues/1",
		strings.NewReader(`{"state":"merged"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestUpdateIssue_CloseIssue_200 verifies the full close-issue flow: create an issue,
// then close it via PATCH; the response must return HTTP 200 with state "closed".
func TestUpdateIssue_CloseIssue_200(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoName := "testrepo_" + suffix
	testutil.SeedRepo(t, db, ownerID, ownerName, suffix)

	h := newIssueHandler(db)
	router := issueRouterWithAuth(h)
	token := makeIssueJWT(t, ownerID, ownerName)

	// Step 1: Create an issue.
	createReq := httptest.NewRequest(http.MethodPost,
		"/api/repos/"+ownerName+"/"+repoName+"/issues",
		createIssueBody("Closeable Issue", "body", "public"))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+token)
	createRR := httptest.NewRecorder()
	router.ServeHTTP(createRR, createReq)

	if createRR.Code != http.StatusCreated {
		t.Fatalf("create issue: want 201, got %d: %s", createRR.Code, createRR.Body.String())
	}
	var created map[string]any
	if err := json.NewDecoder(createRR.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	number := int(created["number"].(float64))

	// Step 2: Close the issue.
	closeReq := httptest.NewRequest(http.MethodPatch,
		"/api/repos/"+ownerName+"/"+repoName+"/issues/"+itoa(number),
		strings.NewReader(`{"state":"closed"}`))
	closeReq.Header.Set("Content-Type", "application/json")
	closeReq.Header.Set("Authorization", "Bearer "+token)
	closeRR := httptest.NewRecorder()
	router.ServeHTTP(closeRR, closeReq)

	if closeRR.Code != http.StatusOK {
		t.Fatalf("close issue: want 200, got %d: %s", closeRR.Code, closeRR.Body.String())
	}
	var updated map[string]any
	if err := json.NewDecoder(closeRR.Body).Decode(&updated); err != nil {
		t.Fatalf("decode close response: %v", err)
	}
	if updated["state"] != "closed" {
		t.Errorf("want state closed, got %v", updated["state"])
	}
}

// itoa converts an int to its decimal string representation.
// Used to build URL paths in tests without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	digits := make([]byte, 0, 10)
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}
