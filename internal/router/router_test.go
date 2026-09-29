package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/router"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// Requires TEST_DATABASE_DSN: RequireSetup sends every request to /setup until a user exists.
func TestLegacyPageRedirects(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	username := "testuser_" + suffix

	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "http://localhost:8080"},
		Auth:   config.AuthConfig{JWTSecret: "test-router-secret-32bytes-min!!", JWTExpiry: time.Hour, CookieName: "cz_token_test"},
		Git:    config.GitConfig{ReposRoot: t.TempDir()},
	}
	svc := service.New(store.New(db), cfg)
	mux := router.New(svc, cfg, fstest.MapFS{})
	token, err := svc.User.GenerateTokenForUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("token: %v", err)
	}

	tests := []struct{ request, want string }{
		{"/new", "/repos/new"},
		{"/new?owner=acme", "/repos/new?owner=acme"},
		{"/settings/organizations", "/organizations"},
		{"/settings/security", "/settings#security"},
		{"/settings/notifications", "/settings#notifications"},
		{"/settings/tokens", "/settings#tokens"},
		{"/settings/replies", "/settings#saved-replies"},
		{"/settings/oauth-apps", "/settings#oauth-apps"},
		{"/" + username + "/gists", "/" + username + "?tab=gists"},
		{"/" + username + "/gists?page=2", "/" + username + "?page=2&tab=gists"},
		{"/" + username + "/stars", "/" + username + "?tab=stars"},
	}
	for _, tc := range tests {
		t.Run(tc.request, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.request, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)
			if rr.Code != http.StatusMovedPermanently {
				t.Fatalf("status = %d, want 301", rr.Code)
			}
			if loc := rr.Header().Get("Location"); loc != tc.want {
				t.Errorf("Location = %q, want %q", loc, tc.want)
			}
		})
	}

	t.Run("settings redirects stay behind login", func(t *testing.T) {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/settings/tokens", nil))
		if rr.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303", rr.Code)
		}
		if loc, want := rr.Header().Get("Location"), "/login?next=%2Fsettings%2Ftokens"; loc != want {
			t.Errorf("Location = %q, want %q", loc, want)
		}
	})

	t.Run("POST routes on moved paths are not shadowed", func(t *testing.T) {
		routes := mux.(chi.Routes)
		for _, path := range []string{"/settings/notifications", "/settings/security/setup"} {
			if !routes.Match(chi.NewRouteContext(), http.MethodPost, path) {
				t.Errorf("POST %s no longer routed", path)
			}
		}
	})
}

func TestSettingsExportStubRemoved(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "http://localhost:8080"},
		Auth:   config.AuthConfig{JWTSecret: "test-router-secret-32bytes-min!!", CookieName: "cz_token_test"},
	}
	routes := router.New(&service.Services{}, cfg, fstest.MapFS{}).(chi.Routes)
	rctx := chi.NewRouteContext()
	if routes.Match(rctx, http.MethodPost, "/settings/export") {
		t.Errorf("POST /settings/export is still routed (pattern %q)", rctx.RoutePattern())
	}
}
