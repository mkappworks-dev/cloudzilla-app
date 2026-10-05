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

const trustedProxy = "10.0.0.1"

// SMTP is left unset, so /register serves the classic form.
func newProxiedRouter(t *testing.T) http.Handler {
	t.Helper()
	db := testutil.OpenTestDB(t)
	// Until a user exists every route redirects to /setup.
	testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "http://localhost", TrustedProxies: []string{trustedProxy}},
		Auth:   config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: time.Hour, CookieName: "cz_token"},
		Git:    config.GitConfig{ReposRoot: t.TempDir()},
	}
	h, err := router.New(service.New(store.New(db), cfg), cfg, fstest.MapFS{})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	return h
}

// postEmpty sends an empty form, so a request that reaches its handler fails
// validation and writes nothing.
func postEmpty(h http.Handler, path, remoteAddr, forwardedFor string) int {
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "t"})
	req.Header.Set("X-CSRF-Token", "t")
	req.RemoteAddr = remoteAddr
	if forwardedFor != "" {
		req.Header.Set("X-Forwarded-For", forwardedFor)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr.Code
}

// One client throughout, so each route passing its own limit also shows the
// routes don't share a budget.
func TestRouter_RateLimitsAccountAndLoginRoutes(t *testing.T) {
	h := newProxiedRouter(t)
	const client = "192.0.2.1:1234"

	for _, tc := range []struct {
		path  string
		limit int
	}{
		{"/register", 10},
		{"/register/complete/x", 10},
		{"/login", 30},
	} {
		t.Run(tc.path, func(t *testing.T) {
			for i := range tc.limit {
				if code := postEmpty(h, tc.path, client, ""); code == http.StatusTooManyRequests {
					t.Fatalf("request %d was limited", i+1)
				}
			}
			if code := postEmpty(h, tc.path, client, ""); code != http.StatusTooManyRequests {
				t.Errorf("request %d: want 429, got %d", tc.limit+1, code)
			}
		})
	}
}

func TestRouter_RateLimitsForwardedClientBehindTrustedProxy(t *testing.T) {
	h := newProxiedRouter(t)
	const proxy = trustedProxy + ":1234"

	for range 10 {
		postEmpty(h, "/register", proxy, "198.51.100.1")
	}
	if code := postEmpty(h, "/register", proxy, "198.51.100.1"); code != http.StatusTooManyRequests {
		t.Fatalf("precondition: want the forwarded client limited, got %d", code)
	}
	if code := postEmpty(h, "/register", proxy, "198.51.100.2"); code == http.StatusTooManyRequests {
		t.Error("another client behind the proxy must have its own budget")
	}
}

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
	mux, err := router.New(svc, cfg, fstest.MapFS{})
	if err != nil {
		t.Fatalf("router: %v", err)
	}
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
		{"/settings/appearance", "/settings#appearance"},
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
		for _, path := range []string{"/settings/notifications", "/settings/appearance", "/settings/security/setup"} {
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
	h, err := router.New(&service.Services{}, cfg, fstest.MapFS{})
	if err != nil {
		t.Fatalf("router: %v", err)
	}
	routes := h.(chi.Routes)
	rctx := chi.NewRouteContext()
	if routes.Match(rctx, http.MethodPost, "/settings/export") {
		t.Errorf("POST /settings/export is still routed (pattern %q)", rctx.RoutePattern())
	}
}
