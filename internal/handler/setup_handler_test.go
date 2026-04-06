package handler_test

// Integration tests for setup handler. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mkappworks/cloudzilla/internal/config"
	"github.com/mkappworks/cloudzilla/internal/handler"
	"github.com/mkappworks/cloudzilla/internal/service"
	"github.com/mkappworks/cloudzilla/internal/store"
	"github.com/mkappworks/cloudzilla/internal/testutil"
)

// newSetupHandlerWithDB builds a Handler with only the services needed by PageSetup and PageSetupSubmit.
func newSetupHandlerWithDB(db *sql.DB) *handler.Handler {
	stores := &store.Stores{
		User:        store.NewUserStore(db),
		SiteSetting: store.NewSiteSettingStore(db),
		AuditLog:    store.NewAuditLogStore(db),
	}
	cfg := &config.Config{
		Auth: config.AuthConfig{
			JWTSecret:  testJWTSecret,
			JWTExpiry:  24 * time.Hour,
			CookieName: testCookieName,
		},
	}
	svc := &service.Services{
		User:        service.NewUserService(stores.User, cfg.Auth),
		SiteSetting: service.NewSiteSettingService(stores.SiteSetting, stores.User),
		AuditLog:    service.NewAuditService(stores.AuditLog),
	}
	return handler.New(svc, cfg)
}

// TestPageSetup_SetupComplete_RedirectsToRoot verifies that when at least one user
// exists in the database (setup is considered complete), GET /setup redirects to root.
func TestPageSetup_SetupComplete_RedirectsToRoot(t *testing.T) {
	db := testutil.OpenTestDB(t)
	// Seeding a user causes IsSetupComplete to return true.
	testutil.SeedUser(t, db, testutil.UniqueSuffix(t))

	h := newSetupHandlerWithDB(db)
	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	rr := httptest.NewRecorder()
	h.PageSetup(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("want 303, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/" {
		t.Errorf("want redirect to /, got %q", loc)
	}
}

// TestPageSetupSubmit_SetupComplete_RedirectsToRoot verifies that when setup is already
// complete, POST /setup redirects to root immediately without attempting user creation.
func TestPageSetupSubmit_SetupComplete_RedirectsToRoot(t *testing.T) {
	db := testutil.OpenTestDB(t)
	testutil.SeedUser(t, db, testutil.UniqueSuffix(t))

	h := newSetupHandlerWithDB(db)
	req := httptest.NewRequest(http.MethodPost, "/setup", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.PageSetupSubmit(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("want 303, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/" {
		t.Errorf("want redirect to /, got %q", loc)
	}
}
