package handler_test

// Integration tests for org handlers. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newOrgTestRouter(t *testing.T, db *sql.DB) (http.Handler, *service.Services) {
	t.Helper()
	cfg := &config.Config{
		Auth: config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName},
		Git:  config.GitConfig{ReposRoot: t.TempDir()},
	}
	svc := service.New(store.New(db), cfg)
	h := handler.New(svc, cfg)
	r := chi.NewRouter()
	r.Post("/api/orgs/{org}/transfer", h.TransferOrg)
	r.Get("/repos/new", h.PageNewRepo)
	unauthorized := func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) }
	return middleware.Auth(testJWTSecret, testCookieName, nil, nil, unauthorized)(r), svc
}

// The previous owner is demoted to member by the transfer, so sending them
// back to the owner-only settings page would land on a 403.
func TestTransferOrg_RedirectsToOrgPage(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	newOwnerSuffix := suffix + "_new"
	testutil.SeedUser(t, db, newOwnerSuffix)
	router, svc := newOrgTestRouter(t, db)

	org, err := svc.Org.Create(context.Background(), ownerID, "testorg_"+suffix, "", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	form := url.Values{"new_owner": {"testuser_" + newOwnerSuffix}, "confirm_name": {org.Name}}
	req := httptest.NewRequest(http.MethodPost, "/api/orgs/"+org.Name+"/transfer", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+makeIssueJWT(t, ownerID, "testuser_"+suffix))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d: %s", rr.Code, rr.Body.String())
	}
	if loc := rr.Header().Get("Location"); loc != "/"+org.Name {
		t.Errorf("Location = %q, want %q", loc, "/"+org.Name)
	}
}

var checkedPrivateRadio = regexp.MustCompile(`<input type="radio" name="visibility" value="private" checked`)

// Arriving from an org page (?owner=org) must preselect the org's default visibility.
func TestPageNewRepo_OwnerOrgDefaultVisibility(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	router, svc := newOrgTestRouter(t, db)
	ctx := context.Background()

	org, err := svc.Org.Create(ctx, ownerID, "testorg_"+suffix, "", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	get := func(query string) string {
		req := httptest.NewRequest(http.MethodGet, "/repos/new"+query, nil)
		req.Header.Set("Authorization", "Bearer "+makeIssueJWT(t, ownerID, "testuser_"+suffix))
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET /repos/new%s: want 200, got %d", query, rr.Code)
		}
		return rr.Body.String()
	}

	if err := svc.Org.UpdateRepoDefaults(ctx, org.ID, ownerID, "private", "main"); err != nil {
		t.Fatalf("UpdateRepoDefaults: %v", err)
	}
	if !checkedPrivateRadio.MatchString(get("?owner=" + org.Name)) {
		t.Error("private-by-default org: Private radio not preselected")
	}
	if checkedPrivateRadio.MatchString(get("")) {
		t.Error("personal owner: Private radio preselected, want Public")
	}
	if !checkedPrivateRadio.MatchString(get("?owner=" + org.Name + "&visibility=private")) {
		t.Error("explicit ?visibility=private ignored")
	}

	if err := svc.Org.UpdateRepoDefaults(ctx, org.ID, ownerID, "public", "main"); err != nil {
		t.Fatalf("UpdateRepoDefaults: %v", err)
	}
	if checkedPrivateRadio.MatchString(get("?owner=" + org.Name)) {
		t.Error("public-by-default org: Private radio preselected")
	}
}
