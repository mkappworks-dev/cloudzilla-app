package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
)

func TestMovedPermanently(t *testing.T) {
	tests := []struct {
		name, path, fragment, request, want string
	}{
		{"plain", "/repos/new", "", "/new", "/repos/new"},
		{"keeps query", "/repos/new", "", "/new?owner=acme&init_readme=1", "/repos/new?init_readme=1&owner=acme"},
		{"fragment", "/settings", "tokens", "/settings/tokens", "/settings#tokens"},
		{"query before fragment", "/settings", "tokens", "/settings/tokens?new_token=abc", "/settings?new_token=abc#tokens"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			handler.MovedPermanently(tc.path, tc.fragment)(rr, httptest.NewRequest(http.MethodGet, tc.request, nil))
			if rr.Code != http.StatusMovedPermanently {
				t.Errorf("status = %d, want 301", rr.Code)
			}
			if loc := rr.Header().Get("Location"); loc != tc.want {
				t.Errorf("Location = %q, want %q", loc, tc.want)
			}
		})
	}
}

func TestMovedToProfileTab(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/{owner}/gists", handler.MovedToProfileTab("gists"))
	tests := []struct {
		name, request, want string
	}{
		{"plain", "/alice/gists", "/alice?tab=gists"},
		{"keeps page", "/alice/gists?page=3", "/alice?page=3&tab=gists"},
		{"tab cannot be overridden", "/alice/gists?tab=repositories", "/alice?tab=gists"},
		{"backslash owner stays on-site", `/%5Cevil.com/gists`, "/%5Cevil.com?tab=gists"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, tc.request, nil))
			if rr.Code != http.StatusMovedPermanently {
				t.Errorf("status = %d, want 301", rr.Code)
			}
			if loc := rr.Header().Get("Location"); loc != tc.want {
				t.Errorf("Location = %q, want %q", loc, tc.want)
			}
		})
	}
}
