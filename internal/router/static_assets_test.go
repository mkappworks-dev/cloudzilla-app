package router_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/mkappworks-dev/cloudzilla-app/internal/assets"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/router"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// No database and no account: the setup page loads these assets too, so
// RequireSetup must pass them straight through.
func TestRouter_TemplateAssetURLsAreCachedForGood(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "http://localhost"},
		Auth:   config.AuthConfig{JWTSecret: testJWTSecret},
	}
	frontend := fstest.MapFS{
		"frontend/static/main.css": {Data: []byte("body{color:red}")},
		"frontend/htmx.min.js":     {Data: []byte("htmx")},
		"frontend/alpine.min.js":   {Data: []byte("alpine")},
	}
	h, err := router.New(service.New(store.New(nil), cfg), cfg, frontend)
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	t.Cleanup(func() { assets.SetDefault(nil) })

	for path, want := range map[string]string{
		"/static/main.css": "/static/main.css?v=15c42ab7768d955e",
		"/htmx.min.js":     "/htmx.min.js?v=dc476210dea6474d",
		"/alpine.min.js":   "/alpine.min.js?v=54c5b3dd459d5ef7",
	} {
		url := assets.URL(path)
		if url != want {
			t.Errorf("assets.URL(%q) = %q, want %q", path, url, want)
			continue
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, url, nil))
		if rr.Code != http.StatusOK {
			t.Errorf("GET %s: status %d, want 200", url, rr.Code)
		}
		if got := rr.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
			t.Errorf("GET %s: Cache-Control %q, want a year-long immutable cache", url, got)
		}
	}
}
