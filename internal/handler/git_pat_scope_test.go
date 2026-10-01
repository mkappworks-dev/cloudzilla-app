package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// git's Basic-auth path skips the auth middleware, so it checks a token's
// scopes itself: a read-only token fetches but doesn't push.
func TestResolveGitUser_HoldsTokensToTheirScopes(t *testing.T) {
	db := testutil.OpenTestDB(t)
	cfg := &config.Config{Auth: config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!", JWTExpiry: time.Hour}}
	h := New(service.New(store.New(db), cfg), cfg)
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))

	for _, tt := range []struct {
		name        string
		scopes      []string
		fetch, push bool
	}{
		{"no scopes", nil, true, true},
		{"repo:read", []string{model.ScopeRepoRead}, true, false},
		{"repo:write", []string{model.ScopeRepoWrite}, true, true},
		{"issues:write", []string{model.ScopeIssuesWrite}, true, false},
	} {
		raw, _, err := h.Services.AccessToken.Generate(t.Context(), userID, tt.name, tt.scopes, nil)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		for push, want := range map[bool]bool{false: tt.fetch, true: tt.push} {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.SetBasicAuth("x", raw)
			if got := h.resolveGitUser(req, push) != nil; got != want {
				t.Errorf("%s token, push=%v: authenticated = %v, want %v", tt.name, push, got, want)
			}
		}
	}
}
