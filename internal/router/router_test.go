package router_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

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
