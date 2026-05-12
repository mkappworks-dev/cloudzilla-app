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

// newAdminHandler builds a Handler with the services needed by admin endpoints.
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

// makeSuperadminJWT creates a signed HS256 JWT with IsSuperadmin=true.
func makeSuperadminJWT(t *testing.T, userID int64, username string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"sub":          float64(userID),
		"username":     username,
		"is_superadmin": true,
		"exp":          float64(time.Now().Add(time.Hour).Unix()),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("makeSuperadminJWT: %v", err)
	}
	return s
}

// withAuth returns a new http.Handler that wraps h in the Auth middleware.
func withAuth(h *handler.Handler) http.Handler {
	return middleware.Auth(testJWTSecret, testCookieName, nil, nil)(
		http.HandlerFunc(h.UpdateSiteSetting),
	)
}

// TestUpdateSiteSetting_NoAuth_403 verifies that UpdateSiteSetting returns HTTP 403
// when no Authorization header is present (treated as non-superadmin).
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

// TestUpdateSiteSetting_RegularUser_403 verifies that a non-superadmin user receives
// HTTP 403 when attempting to update a site setting.
func TestUpdateSiteSetting_RegularUser_403(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)

	h := newAdminHandler(db)
	// Use a regular (non-superadmin) JWT via Auth middleware.
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

// TestUpdateSiteSetting_Superadmin_200 verifies that a superadmin can update a
// site setting and receives HTTP 200 (non-HTMX response).
func TestUpdateSiteSetting_Superadmin_200(t *testing.T) {
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

	if rr.Code != http.StatusOK {
		t.Errorf("want 200 for superadmin, got %d: %s", rr.Code, rr.Body.String())
	}
}
