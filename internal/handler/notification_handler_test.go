package handler_test

// Integration tests for notification handler. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// newNotifHandler builds a Handler with only the Notification service and auth config.
func newNotifHandler(db *sql.DB) *handler.Handler {
	cfg := &config.Config{
		Auth: config.AuthConfig{
			JWTSecret:  testJWTSecret,
			JWTExpiry:  24 * time.Hour,
			CookieName: testCookieName,
		},
	}
	userSvc := service.NewUserService(store.NewUserStore(db), cfg.Auth)
	svc := &service.Services{
		Notification: service.NewNotificationService(
			store.NewNotificationStore(db),
			store.NewWatchStore(db),
			service.NewEmailService(config.SMTPConfig{}),
			userSvc,
		),
		SiteSetting: service.NewSiteSettingService(store.NewSiteSettingStore(db), store.NewUserStore(db)),
		AuditLog:    service.NewAuditService(store.NewAuditLogStore(db)),
	}
	return handler.New(svc, cfg)
}

// notifRouter wraps notification handler methods in a chi.Mux.
func notifRouter(h *handler.Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Patch("/api/notifications/{id}/read", h.MarkNotificationRead)
	r.Post("/api/notifications/read-all", h.MarkAllNotificationsRead)
	r.Get("/api/notifications/unread-count", h.GetUnreadCount)
	return r
}

// notifRouterWithAuth wraps the notification router in Auth middleware.
func notifRouterWithAuth(h *handler.Handler) http.Handler {
	return middleware.Auth(testJWTSecret, testCookieName, nil, nil, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) })(notifRouter(h))
}

// seedNotification inserts a single unread notification for the given user and returns its ID.
func seedNotification(t *testing.T, db *sql.DB, userID, actorID, repoID int64, ownerName, repoName string) int64 {
	t.Helper()
	ns := store.NewNotificationStore(db)
	n := &model.Notification{
		UserID:    userID,
		ActorID:   actorID,
		ActorName: ownerName,
		Type:      model.NotifIssueComment,
		RepoID:    repoID,
		RepoName:  repoName,
		OwnerName: ownerName,
		SubjectID: 1,
	}
	if err := ns.Create(t.Context(), n); err != nil {
		t.Fatalf("seedNotification: %v", err)
	}
	return n.ID
}

// TestGetUnreadCount_NoAuth_401 verifies that GET /api/notifications/unread-count
// returns HTTP 401 when no Authorization header is present.
func TestGetUnreadCount_NoAuth_401(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newNotifHandler(db)
	router := notifRouterWithAuth(h)

	req := httptest.NewRequest(http.MethodGet, "/api/notifications/unread-count", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rr.Code)
	}
}

// TestGetUnreadCount_Authenticated_ReturnsCount verifies that an authenticated request
// returns HTTP 200 with a JSON object containing the "count" field.
func TestGetUnreadCount_Authenticated_ReturnsCount(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)

	h := newNotifHandler(db)
	router := notifRouterWithAuth(h)
	token := makeIssueJWT(t, userID, "testuser_"+suffix)

	req := httptest.NewRequest(http.MethodGet, "/api/notifications/unread-count", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var body map[string]int
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["count"]; !ok {
		t.Error("response must contain 'count' field")
	}
}

// TestMarkAllNotificationsRead_NoAuth_401 verifies that POST /api/notifications/read-all
// returns HTTP 401 when no Authorization header is present.
func TestMarkAllNotificationsRead_NoAuth_401(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newNotifHandler(db)
	router := notifRouterWithAuth(h)

	req := httptest.NewRequest(http.MethodPost, "/api/notifications/read-all", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rr.Code)
	}
}

// TestMarkAllNotificationsRead_Authenticated_204 verifies that an authenticated POST
// to /api/notifications/read-all marks all notifications read and returns HTTP 204.
func TestMarkAllNotificationsRead_Authenticated_204(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	actorID := testutil.SeedUser(t, db, "actor_n_"+suffix)
	repoID := testutil.SeedRepo(t, db, userID, "testuser_"+suffix, suffix)
	seedNotification(t, db, userID, actorID, repoID, "testuser_"+suffix, "testrepo_"+suffix)

	h := newNotifHandler(db)
	router := notifRouterWithAuth(h)
	token := makeIssueJWT(t, userID, "testuser_"+suffix)

	req := httptest.NewRequest(http.MethodPost, "/api/notifications/read-all", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("want 204, got %d: %s", rr.Code, rr.Body.String())
	}
}
