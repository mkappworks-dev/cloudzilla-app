package router_test

// Integration tests for the global rate limiter through the real route table.
// They require TEST_DATABASE_DSN and skip otherwise.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/router"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const rateLimitedClient = "192.0.2.50:1234"

func rateLimitedRequest(h http.Handler, method, path string, setup func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = rateLimitedClient
	setup(req)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestRouter_RateLimitBuckets(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	username := "testuser_" + suffix
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "http://localhost"},
		Auth:   config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: time.Hour, CookieName: "cz_token"},
		Git:    config.GitConfig{ReposRoot: t.TempDir()},
		RateLimit: config.RateLimitConfig{
			Enabled: true,
			Window:  time.Hour,
			Core:    config.RateBudget{Authenticated: 3, Anonymous: 3},
			Git:     config.RateBudget{Authenticated: 3, Anonymous: 3},
		},
	}
	svc := service.New(store.New(db), cfg)
	h, err := router.New(svc, cfg, fstest.MapFS{})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	repoName := seedPrivateRepo(t, svc, userID, username, suffix)
	patA := mintPAT(t, svc, userID, model.ScopeRepoRead)
	patB := mintPAT(t, svc, userID, model.ScopeRepoRead)
	session := makeJWT(t, userID, username)
	apiPath := "/api/repos/" + username + "/" + repoName
	gitPath := "/" + username + "/" + repoName + "/info/refs?service=git-upload-pack"

	anonymous := func(*http.Request) {}
	withSession := func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "cz_token", Value: session}) }
	withBearer := func(tok string) func(*http.Request) {
		return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok) }
	}
	withGitBasic := func(r *http.Request) { r.SetBasicAuth(username, patA) }

	steps := []struct {
		name      string
		method    string
		path      string
		setup     func(*http.Request)
		resource  string
		remaining string
	}{
		{"anonymous page", "GET", "/explore", anonymous, "core", "2"},
		{"session page", "GET", "/explore", withSession, "core", "2"},
		{"first PAT", "GET", apiPath, withBearer(patA), "core", "2"},
		{"second PAT shares the token bucket", "GET", apiPath, withBearer(patB), "core", "1"},
		{"session bucket is untouched by PATs", "GET", "/explore", withSession, "core", "1"},
		{"anonymous git", "GET", gitPath, anonymous, "git", "2"},
		{"git Basic PAT uses the token bucket", "GET", gitPath, withGitBasic, "git", "2"},
		{"git Bearer PAT shares it", "GET", gitPath, withBearer(patB), "git", "1"},
		{"invalid PAT counts against the IP", "GET", apiPath, withBearer("czp_invalid"), "core", "1"},
	}
	for _, s := range steps {
		rr := rateLimitedRequest(h, s.method, s.path, s.setup)
		if rr.Code == http.StatusTooManyRequests {
			t.Fatalf("%s: unexpectedly limited", s.name)
		}
		if got := rr.Header().Get("X-RateLimit-Resource"); got != s.resource {
			t.Errorf("%s: want resource %q, got %q", s.name, s.resource, got)
		}
		if got := rr.Header().Get("X-RateLimit-Remaining"); got != s.remaining {
			t.Errorf("%s: want %s remaining, got %q", s.name, s.remaining, got)
		}
	}

	rateLimitedRequest(h, "GET", apiPath, withBearer(patA))
	rr := rateLimitedRequest(h, "GET", apiPath, withBearer(patA))
	if rr.Code != http.StatusTooManyRequests || rr.Header().Get("Retry-After") == "" {
		t.Fatalf("spent token bucket: want 429 with Retry-After, got %d %v", rr.Code, rr.Header())
	}
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil || body["error"] != "rate limit exceeded" {
		t.Errorf("API refusal: want a JSON error, got %q", rr.Body.String())
	}

	rateLimitedRequest(h, "GET", gitPath, anonymous)
	rateLimitedRequest(h, "GET", gitPath, anonymous)
	rr = rateLimitedRequest(h, "GET", gitPath, anonymous)
	if rr.Code != http.StatusTooManyRequests || !strings.HasPrefix(rr.Header().Get("Content-Type"), "text/plain") {
		t.Errorf("git refusal: want a plain-text 429, got %d %q", rr.Code, rr.Header().Get("Content-Type"))
	}

	if rr := rateLimitedRequest(h, "GET", "/static/app.css", anonymous); rr.Header().Get("X-RateLimit-Limit") != "" {
		t.Error("static assets carry no rate-limit headers")
	}
}

func TestRouter_RateLimitDisabled(t *testing.T) {
	h, _, db := newTestRouter(t)
	testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	rr := rateLimitedRequest(h, "GET", "/explore", func(*http.Request) {})
	if rr.Header().Get("X-RateLimit-Limit") != "" {
		t.Errorf("with rate_limit.enabled false, want no rate-limit headers, got %v", rr.Header())
	}
}
