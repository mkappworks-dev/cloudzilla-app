package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func htmlSection(t *testing.T, body, id string) string {
	t.Helper()
	start := strings.Index(body, `<section id="`+id+`"`)
	if start < 0 {
		t.Fatalf("page has no #%s section", id)
	}
	end := strings.Index(body[start:], "</section>")
	if end < 0 {
		t.Fatalf("#%s section is not closed", id)
	}
	return body[start : start+end]
}

func TestPageSettings_ListsRepliesAppsAndAuthorizations(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	token := makeIssueJWT(t, userID, "testuser_"+suffix)

	cfg := &config.Config{
		Auth: config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName},
		Git:  config.GitConfig{ReposRoot: t.TempDir()},
	}
	svc := service.New(store.New(db), cfg)
	h := handler.New(svc, cfg)
	r := chi.NewRouter()
	r.Get("/settings", h.PageSettings)
	r.Delete("/api/oauth/authorizations/{id}", h.RevokeOAuthAuthorization)
	unauthorized := func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) }
	router := middleware.Auth(testJWTSecret, testCookieName, nil, nil, unauthorized)(r)

	reply, err := svc.SavedReply.Create(ctx, userID, "LGTM "+suffix, "Looks good to me.")
	if err != nil {
		t.Fatalf("create saved reply: %v", err)
	}
	app, _, err := svc.OAuthApp.CreateApp(ctx, userID, "Deploy bot "+suffix, "", "", []string{"https://example.test/cb"})
	if err != nil {
		t.Fatalf("create oauth app: %v", err)
	}
	if _, err := svc.OAuthApp.Authorize(ctx, app.ID, userID, "https://example.test/cb", []string{"repo"}, app); err != nil {
		t.Fatalf("authorize: %v", err)
	}
	auths, err := svc.OAuthApp.ListAuthorizationsByUser(ctx, userID)
	if err != nil || len(auths) != 1 {
		t.Fatalf("ListAuthorizationsByUser = %d, %v; want 1", len(auths), err)
	}
	revokePath := "/api/oauth/authorizations/" + strconv.FormatInt(auths[0].ID, 10)

	serve := func(method, path string, htmx bool) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		if htmx {
			req.Header.Set("HX-Request", "true")
		}
		req.AddCookie(&http.Cookie{Name: testCookieName, Value: token})
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s %s: want 200, got %d: %s", method, path, rr.Code, rr.Body.String())
		}
		return rr
	}

	page := serve(http.MethodGet, "/settings", false).Body.String()
	if replies := htmlSection(t, page, "saved-replies"); !strings.Contains(replies, reply.Title) {
		t.Errorf("saved replies section does not list %q", reply.Title)
	}
	oauth := htmlSection(t, page, "oauth-apps")
	if !strings.Contains(oauth, app.ClientID) {
		t.Errorf("OAuth apps section does not list app %s", app.ClientID)
	}
	if !strings.Contains(oauth, `hx-delete="`+revokePath+`"`) {
		t.Errorf("OAuth apps section has no revoke control for %s", revokePath)
	}

	revoked := serve(http.MethodDelete, revokePath, true).Body.String()
	if !strings.Contains(revoked, `id="oauth-authorizations-list"`) {
		t.Errorf("revoke response is not the authorizations list fragment: %s", revoked)
	}
	if strings.Contains(revoked, revokePath) {
		t.Error("revoke response still lists the revoked authorization")
	}
	if auths, err := svc.OAuthApp.ListAuthorizationsByUser(ctx, userID); err != nil || len(auths) != 0 {
		t.Errorf("after revoke: %d authorizations, %v; want 0", len(auths), err)
	}
}
