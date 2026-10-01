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
	oauthSvc := service.NewOAuthAppService(store.NewOAuthAppStore(db), store.NewOAuthAuthorizationStore(db), store.NewUserStore(db))
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

func TestSafeNextPath(t *testing.T) {
	tests := []struct{ next, want string }{
		{"/oauth/authorize?client_id=a&state=b", "/oauth/authorize?client_id=a&state=b"},
		{"", "/"},
		{"settings", "/"},
		{"https://evil.example/", "/"},
		{"//evil.example/", "/"},
		{`/\evil.example/`, "/"},
		{"/\t/evil.example/", "/"},
		{"/\n/evil.example/", "/"},
		{`/./\evil.example/`, "/"},
		{`/a/../\evil.example/`, "/"},
		{`/../\evil.example/`, "/"},
		{`/oauth/authorize?state=a\b`, `/oauth/authorize?state=a\b`},
	}
	for _, tc := range tests {
		got := handler.SafeNextPath(tc.next)
		if got != tc.want {
			t.Errorf("safeNextPath(%q) = %q, want %q", tc.next, got, tc.want)
		}
		rr := httptest.NewRecorder()
		http.Redirect(rr, httptest.NewRequest(http.MethodGet, "/login", nil), got, http.StatusSeeOther)
		if loc := rr.Header().Get("Location"); strings.HasPrefix(loc, "//") || strings.HasPrefix(loc, `/\`) {
			t.Errorf("safeNextPath(%q) redirects off-site: Location %q", tc.next, loc)
		}
	}
}

func TestPageOAuthAuthorize_SignedOutSendsWholeRequestAsNext(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	oauthSvc := service.NewOAuthAppService(store.NewOAuthAppStore(db), store.NewOAuthAuthorizationStore(db), store.NewUserStore(db))
	app, _, err := oauthSvc.CreateApp(t.Context(), userID, "Next app "+suffix, "", "", []string{"https://client.example/cb"})
	if err != nil {
		t.Fatalf("create oauth app: %v", err)
	}
	h := handler.New(&service.Services{OAuthApp: oauthSvc}, &config.Config{})

	authorize := "/oauth/authorize?" + url.Values{
		"client_id":    {app.ClientID},
		"redirect_uri": {"https://client.example/cb"},
		"state":        {"x&code=evil"},
		"next":         {"https://evil.example/"},
	}.Encode()
	for _, tc := range []struct{ name, target, wantNext string }{
		{"whole authorize URL", authorize, authorize},
		{"off-site path", "//evil.example" + authorize, "/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			h.PageOAuthAuthorize(rr, httptest.NewRequest(http.MethodGet, tc.target, nil))
			if rr.Code != http.StatusSeeOther {
				t.Fatalf("want 303, got %d: %s", rr.Code, rr.Body.String())
			}
			loc, err := url.Parse(rr.Header().Get("Location"))
			if err != nil || loc.Path != "/login" {
				t.Fatalf("redirected to %q, want /login", rr.Header().Get("Location"))
			}
			if q := loc.Query(); len(q) != 1 || len(q["next"]) != 1 || q.Get("next") != tc.wantNext {
				t.Errorf("login query = %v, want only next=%q", q, tc.wantNext)
			}
		})
	}
}

func TestConfirmAuthorize_StateCannotAddRedirectParams(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID, _ := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	token := makeIssueJWT(t, userID, "testpw_"+suffix)

	const registered = "https://client.example/cb?tenant=acme"
	const state = "x&code=evil&state=y"
	oauthSvc := service.NewOAuthAppService(store.NewOAuthAppStore(db), store.NewOAuthAuthorizationStore(db), store.NewUserStore(db))
	app, clientSecret, err := oauthSvc.CreateApp(t.Context(), userID, "State app "+suffix, "", "", []string{registered})
	if err != nil {
		t.Fatalf("create oauth app: %v", err)
	}
	cfg := &config.Config{Auth: config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: time.Hour, CookieName: testCookieName}}
	r := chi.NewRouter()
	users := store.NewUserStore(db)
	reauth := service.NewReauthService(users, service.NewTOTPService(users))
	r.Post("/oauth/authorize", handler.New(&service.Services{OAuthApp: oauthSvc, Reauth: reauth}, cfg).ConfirmAuthorize)
	unauthorized := func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) }
	router := middleware.Auth(testJWTSecret, testCookieName, nil, nil, unauthorized)(r)

	for _, action := range []string{"allow", "deny"} {
		t.Run(action, func(t *testing.T) {
			form := url.Values{"client_id": {app.ClientID}, "redirect_uri": {registered}, "state": {state}, "action": {action}, "password": {"password1"}}
			req := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(&http.Cookie{Name: testCookieName, Value: token})
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
			if rr.Code != http.StatusSeeOther {
				t.Fatalf("want 303, got %d: %s", rr.Code, rr.Body.String())
			}
			loc, err := url.Parse(rr.Header().Get("Location"))
			if err != nil || loc.Scheme != "https" || loc.Host != "client.example" || loc.Path != "/cb" {
				t.Fatalf("redirected to %q, want the registered URI", rr.Header().Get("Location"))
			}
			q := loc.Query()
			if len(q["state"]) != 1 || q.Get("state") != state {
				t.Errorf("state = %q, want exactly %q", q["state"], state)
			}
			if len(q["tenant"]) != 1 || q.Get("tenant") != "acme" {
				t.Errorf("tenant = %q, want the registered URI's own acme", q["tenant"])
			}
			if action == "deny" {
				if q.Get("error") != "access_denied" || q.Has("code") {
					t.Errorf("deny query = %v, want error=access_denied and no code", q)
				}
				return
			}
			if len(q["code"]) != 1 {
				t.Fatalf("code = %q, want exactly one", q["code"])
			}
			if _, err := oauthSvc.ExchangeCode(t.Context(), app.ClientID, clientSecret, q.Get("code"), registered); err != nil {
				t.Errorf("code %q in the redirect is not the issued one: %v", q.Get("code"), err)
			}
		})
	}
}
