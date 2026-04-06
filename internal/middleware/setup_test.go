package middleware_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/service"
	"github.com/mkappworks/cloudzilla/internal/store"
)

// newIncompleteSetupService returns a SiteSettingService backed by an unreachable
// database, so IsSetupComplete always returns false without a real DB.
func newIncompleteSetupService() *service.SiteSettingService {
	// sql.Open does not dial — queries will fail at execution time, causing
	// CountAll to return (0, err), so IsSetupComplete returns false.
	db, _ := sql.Open("pgx", "postgres://localhost:1/nonexistent?connect_timeout=1")
	return service.NewSiteSettingService(
		store.NewSiteSettingStore(db),
		store.NewUserStore(db),
	)
}

func setupHandler() http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func applySetup(svc *service.SiteSettingService, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rr := httptest.NewRecorder()
	middleware.RequireSetup(svc)(setupHandler()).ServeHTTP(rr, req)
	return rr
}

// --- Allowlist paths pass through even when setup is not complete ---

func TestRequireSetup_AllowsSetupPath(t *testing.T) {
	svc := newIncompleteSetupService()
	rr := applySetup(svc, "/setup")
	if rr.Code != http.StatusOK {
		t.Errorf("/setup: want 200, got %d", rr.Code)
	}
}

func TestRequireSetup_AllowsStaticPrefix(t *testing.T) {
	svc := newIncompleteSetupService()
	for _, path := range []string{"/static/main.css", "/static/htmx.min.js", "/static/"} {
		rr := applySetup(svc, path)
		if rr.Code != http.StatusOK {
			t.Errorf("%s: want 200, got %d", path, rr.Code)
		}
	}
}

func TestRequireSetup_AllowsInvitePrefix(t *testing.T) {
	svc := newIncompleteSetupService()
	rr := applySetup(svc, "/invite/abc123")
	if rr.Code != http.StatusOK {
		t.Errorf("/invite/abc123: want 200, got %d", rr.Code)
	}
}

func TestRequireSetup_AllowsHTMX(t *testing.T) {
	svc := newIncompleteSetupService()
	rr := applySetup(svc, "/htmx.min.js")
	if rr.Code != http.StatusOK {
		t.Errorf("/htmx.min.js: want 200, got %d", rr.Code)
	}
}

// --- Non-allowlisted paths redirect when setup is not complete ---

func TestRequireSetup_RedirectsNonAllowlisted(t *testing.T) {
	svc := newIncompleteSetupService()
	paths := []string{"/", "/login", "/api/repos", "/user/settings", "/explore"}
	for _, path := range paths {
		rr := applySetup(svc, path)
		if rr.Code != http.StatusSeeOther {
			t.Errorf("%s: want 303 redirect, got %d", path, rr.Code)
		}
		if loc := rr.Header().Get("Location"); loc != "/setup" {
			t.Errorf("%s: want Location=/setup, got %q", path, loc)
		}
	}
}
