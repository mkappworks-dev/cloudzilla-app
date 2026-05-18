package handler_test

// Integration tests for admin handler. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newAdminHandler(db *sql.DB) *handler.Handler {
	cfg := &config.Config{
		Auth: config.AuthConfig{
			JWTSecret:  testJWTSecret,
			JWTExpiry:  24 * time.Hour,
			CookieName: testCookieName,
		},
	}
	userSvc := service.NewUserService(store.NewUserStore(db), cfg.Auth)
	svc := &service.Services{
		User:        userSvc,
		SiteSetting: service.NewSiteSettingService(store.NewSiteSettingStore(db), store.NewUserStore(db)),
		Invitation:  service.NewInvitationService(store.NewInvitationStore(db)),
		AuditLog:    service.NewAuditService(store.NewAuditLogStore(db)),
	}
	return handler.New(svc, cfg)
}

func makeSuperadminJWT(t *testing.T, userID int64, username string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"sub":           float64(userID),
		"username":      username,
		"is_superadmin": true,
		"exp":           float64(time.Now().Add(time.Hour).Unix()),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("makeSuperadminJWT: %v", err)
	}
	return s
}

func withAuth(h *handler.Handler) http.Handler {
	return middleware.Auth(testJWTSecret, testCookieName, nil, nil, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) })(
		http.HandlerFunc(h.UpdateSiteSetting),
	)
}

func TestUpdateSiteSetting_NoAuth_403(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newAdminHandler(db)

	req := httptest.NewRequest(http.MethodPost, "/admin/settings",
		strings.NewReader("key=allow_registration&value=true"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	// Call the handler directly without auth middleware — no claims in context → 403.
	h.UpdateSiteSetting(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("want 403, got %d", rr.Code)
	}
}

func TestUpdateSiteSetting_RegularUser_403(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)

	h := newAdminHandler(db)
	router := withAuth(h)

	req := httptest.NewRequest(http.MethodPost, "/admin/settings",
		strings.NewReader("key=allow_registration&value=true"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+makeIssueJWT(t, userID, "testuser_"+suffix))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("want 403 for regular user, got %d", rr.Code)
	}
}

func TestUpdateSiteSetting_Superadmin_303(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	adminID := testutil.SeedSuperadmin(t, db, suffix)

	h := newAdminHandler(db)
	router := withAuth(h)

	token := makeSuperadminJWT(t, adminID, "admin_"+suffix)
	req := httptest.NewRequest(http.MethodPost, "/admin/settings",
		strings.NewReader("key=allow_registration&value=true"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("want 303 for superadmin, got %d: %s", rr.Code, rr.Body.String())
	}
}
