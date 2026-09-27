package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestCreateOrg_InvalidName_400WithRule(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM organizations WHERE name = '..'`) })
	h := newAuthHandler(db)
	h.Services.Org = service.NewOrgService(store.NewOrgStore(db), store.NewRepoStore(db), store.NewUserStore(db), config.GitConfig{})
	authMW := middleware.Auth(testJWTSecret, testCookieName, nil, nil, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) })

	req := httptest.NewRequest(http.MethodPost, "/api/orgs", strings.NewReader(`{"name":".."}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeIssueJWT(t, userID, "testuser_"+suffix))
	rr := httptest.NewRecorder()
	authMW(http.HandlerFunc(h.CreateOrg)).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", rr.Code, rr.Body)
	}
	if !strings.Contains(rr.Body.String(), "Organization names can use letters, numbers, - and _") {
		t.Errorf("want the name rule; body: %s", rr.Body)
	}
}
