package router_test

// Integration tests for the OAuth authorization-code flow through the real route table.
// All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type oauthFlow struct {
	h       http.Handler
	svc     *service.Services
	db      *sql.DB
	suffix  string
	userID  int64
	session string
}

func newOAuthFlow(t *testing.T) oauthFlow {
	t.Helper()
	h, svc, db := newTestRouter(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	return oauthFlow{h: h, svc: svc, db: db, suffix: suffix, userID: userID, session: makeJWT(t, userID, "testuser_"+suffix)}
}

func (f oauthFlow) createApp(t *testing.T, redirectURIs ...string) (*model.OAuthApp, string) {
	t.Helper()
	app, secret, err := f.svc.OAuthApp.CreateApp(context.Background(), f.userID, "Flow Test App", "", "", redirectURIs)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	return app, secret
}

// browser sends a request the way the signed-in user's browser would: with
// session and CSRF cookies.
func (f oauthFlow) browser(method, target string, form url.Values) *httptest.ResponseRecorder {
	const csrf = "test-csrf-token"
	var body io.Reader
	if form != nil {
		form.Set("csrf_token", csrf)
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.AddCookie(&http.Cookie{Name: "cz_token", Value: f.session})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	rr := httptest.NewRecorder()
	f.h.ServeHTTP(rr, req)
	return rr
}

func (f oauthFlow) authorizePage(clientID, redirectURI string) *httptest.ResponseRecorder {
	q := url.Values{"client_id": {clientID}, "redirect_uri": {redirectURI}, "scope": {model.ScopeRepoRead}}
	return f.browser(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil)
}

func (f oauthFlow) confirm(clientID, redirectURI, state, action string) *httptest.ResponseRecorder {
	return f.browser(http.MethodPost, "/oauth/authorize", url.Values{
		"client_id":    {clientID},
		"redirect_uri": {redirectURI},
		"scope":        {model.ScopeRepoRead},
		"state":        {state},
		"action":       {action},
	})
}

// token calls /oauth/token as an OAuth client's backend would: no session, no CSRF token.
func (f oauthFlow) token(form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	f.h.ServeHTTP(rr, req)
	return rr
}

func location(t *testing.T, rr *httptest.ResponseRecorder) *url.URL {
	t.Helper()
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d: %s", rr.Code, rr.Body.String())
	}
	u, err := url.Parse(rr.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	return u
}

func TestOAuthAuthorizePage_DeniesFraming(t *testing.T) {
	f := newOAuthFlow(t)
	app, _ := f.createApp(t, testRedirectURI)

	rr := f.authorizePage(app.ClientID, testRedirectURI)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", got)
	}
	if got := rr.Header().Get("Content-Security-Policy"); !strings.Contains(got, "frame-ancestors 'none'") {
		t.Errorf("Content-Security-Policy = %q, want frame-ancestors 'none'", got)
	}
}

// RFC 6749 §4.1.2.1: a bad redirect_uri is reported to the user, never redirected to.
func TestOAuthAuthorize_RejectsUnregisteredRedirectURI(t *testing.T) {
	f := newOAuthFlow(t)
	app, _ := f.createApp(t, testRedirectURI)
	legacy := &model.OAuthApp{OwnerID: f.userID, Name: "Legacy App", ClientID: "legacy_" + f.suffix, ClientSecret: "x"}
	if err := store.NewOAuthAppStore(f.db).Create(context.Background(), legacy); err != nil {
		t.Fatalf("Create: %v", err)
	}

	tests := []struct {
		name        string
		clientID    string
		redirectURI string
	}{
		{"unregistered URI", app.ClientID, "https://attacker.example/cb"},
		{"missing URI", app.ClientID, ""},
		{"app with no registered URIs", legacy.ClientID, "https://attacker.example/cb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rr := f.authorizePage(tt.clientID, tt.redirectURI); rr.Code != http.StatusBadRequest {
				t.Errorf("GET: want 400, got %d", rr.Code)
			}
			rr := f.confirm(tt.clientID, tt.redirectURI, "", "approve")
			if rr.Code != http.StatusBadRequest || rr.Header().Get("Location") != "" {
				t.Errorf("POST: want 400 without redirect, got %d to %q", rr.Code, rr.Header().Get("Location"))
			}
		})
	}
}

func TestOAuthConfirmAuthorize_RedirectKeepsQueryAndEncodesState(t *testing.T) {
	f := newOAuthFlow(t)
	registered := "https://client.example/cb?tenant=acme"
	app, _ := f.createApp(t, registered)
	state := "a&code=forged #frag"

	for _, action := range []string{"approve", "deny"} {
		t.Run(action, func(t *testing.T) {
			loc := location(t, f.confirm(app.ClientID, registered, state, action))
			q := loc.Query()
			if loc.Host != "client.example" || loc.Path != "/cb" || loc.Fragment != "" {
				t.Errorf("redirected to %s, want https://client.example/cb", loc)
			}
			if q.Get("tenant") != "acme" {
				t.Errorf("tenant = %q, want the registered query kept", q.Get("tenant"))
			}
			if q.Get("state") != state {
				t.Errorf("state = %q, want %q", q.Get("state"), state)
			}
			switch action {
			case "approve":
				if len(q["code"]) != 1 || q.Get("code") == "forged" {
					t.Errorf("code = %v, want one issued code", q["code"])
				}
			case "deny":
				if q.Get("error") != "access_denied" || q.Has("code") {
					t.Errorf("query = %v, want error=access_denied and no code", q)
				}
			}
		})
	}
}

func TestOAuthTokenEndpoint_CodeBoundToAppAndRedirectURI(t *testing.T) {
	f := newOAuthFlow(t)
	app, secret := f.createApp(t, testRedirectURI)
	other, otherSecret := f.createApp(t, testRedirectURI)
	code := location(t, f.confirm(app.ClientID, testRedirectURI, "", "approve")).Query().Get("code")

	// In order: refused attempts must leave the code redeemable, and the last row replays it.
	tests := []struct {
		name        string
		clientID    string
		secret      string
		redirectURI string
		want        int
	}{
		{"another app's credentials", other.ClientID, otherSecret, testRedirectURI, http.StatusUnauthorized},
		{"different redirect_uri", app.ClientID, secret, "https://client.example/other", http.StatusUnauthorized},
		{"missing redirect_uri", app.ClientID, secret, "", http.StatusUnauthorized},
		{"issuing app", app.ClientID, secret, testRedirectURI, http.StatusOK},
		{"replayed code", app.ClientID, secret, testRedirectURI, http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := f.token(url.Values{
				"grant_type":    {"authorization_code"},
				"client_id":     {tt.clientID},
				"client_secret": {tt.secret},
				"code":          {code},
				"redirect_uri":  {tt.redirectURI},
			})
			if rr.Code != tt.want {
				t.Fatalf("want %d, got %d: %s", tt.want, rr.Code, rr.Body.String())
			}
			if tt.want != http.StatusOK {
				return
			}
			var resp struct {
				AccessToken string `json:"access_token"`
			}
			if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil || resp.AccessToken == "" {
				t.Errorf("want an access_token, got %s (%v)", rr.Body.String(), err)
			}
		})
	}
}
