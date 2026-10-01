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

// git can't sign its requests, so a token bound to a signing key never
// authenticates it, whatever its scopes.
func TestResolveGitUser_RefusesKeyBoundTokens(t *testing.T) {
	db := testutil.OpenTestDB(t)
	cfg := &config.Config{Auth: config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!", JWTExpiry: time.Hour}}
	h := New(service.New(store.New(db), cfg), cfg)
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	_, pub := testutil.NewSigningKey(t)
	raw, _, err := h.Services.AccessToken.GenerateWithKey(t.Context(), userID, "bound", []string{model.ScopeRepoWrite}, nil, pub)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/acme/app/info/refs?service=git-upload-pack", nil)
	req.SetBasicAuth("x", raw)
	if gu, err := h.resolveGitUser(req); gu != nil || err == nil {
		t.Errorf("a key-bound token authenticated git: user %+v, err %v", gu, err)
	}
}
