package handler_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
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
	"golang.org/x/crypto/bcrypt"
)

var revealedSecret = regexp.MustCompile(`Client secret</dt>\s*<dd><code[^>]*>([^<]+)</code>`)

func TestOAuthAppHTMX_SecretOnlyInCreateResponse(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	token := makeIssueJWT(t, userID, "testuser_"+suffix)

	cfg := &config.Config{
		Auth: config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: time.Hour, CookieName: testCookieName},
	}
	oauthSvc := service.NewOAuthAppService(store.NewOAuthAppStore(db), store.NewOAuthAuthorizationStore(db))
	h := handler.New(&service.Services{OAuthApp: oauthSvc}, cfg)
	r := chi.NewRouter()
	r.Post("/api/oauth/apps", h.CreateOAuthApp)
	r.Delete("/api/oauth/apps/{id}", h.DeleteOAuthApp)
	unauthorized := func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) }
	router := middleware.Auth(testJWTSecret, testCookieName, nil, nil, unauthorized)(r)

	htmx := func(method, path string, form url.Values) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		req.AddCookie(&http.Cookie{Name: testCookieName, Value: token})
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s %s: want 200, got %d: %s", method, path, rr.Code, rr.Body.String())
		}
		return rr
	}

	createRR := htmx(http.MethodPost, "/api/oauth/apps", url.Values{
		"name":         {"CI bot " + suffix},
		"redirect_uri": {"https://example.test/callback"},
	})
	created := createRR.Body.String()
	apps, err := oauthSvc.ListByOwner(t.Context(), userID)
	if err != nil || len(apps) != 1 {
		t.Fatalf("ListByOwner = %d apps, %v; want 1", len(apps), err)
	}
	app := apps[0]
	if len(app.RedirectURIs) != 1 || app.RedirectURIs[0] != "https://example.test/callback" {
		t.Errorf("redirect URIs = %v, want the submitted one", app.RedirectURIs)
	}
	if !strings.Contains(created, "Copy the client secret now") || !strings.Contains(created, app.ClientID) {
		t.Error("create response does not reveal the new client credentials")
	}
	m := revealedSecret.FindStringSubmatch(created)
	if m == nil {
		t.Fatalf("create response has no client secret field:\n%s", created)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(app.ClientSecret), []byte(m[1])); err != nil {
		t.Errorf("revealed client secret %q does not match the stored hash: %v", m[1], err)
	}
	if strings.Contains(created, app.ClientSecret) {
		t.Error("create response contains the stored secret hash")
	}
	if got := createRR.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("create response Cache-Control = %q, want no-store", got)
	}

	deleted := htmx(http.MethodDelete, "/api/oauth/apps/"+strconv.FormatInt(app.ID, 10), nil).Body.String()
	if strings.Contains(deleted, "Copy the client secret now") {
		t.Error("delete response re-renders a client secret")
	}
	if strings.Contains(deleted, app.ClientID) {
		t.Error("delete response still lists the deleted app")
	}
}
