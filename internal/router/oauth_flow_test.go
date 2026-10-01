package router_test

// Integration tests for the OAuth authorization-code flow through the real route table.
// All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	"golang.org/x/oauth2"
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
	userID, _ := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	return oauthFlow{h: h, svc: svc, db: db, suffix: suffix, userID: userID, session: makeJWT(t, userID, "testpw_"+suffix)}
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
		"password":     {"password1"},
	})
}

func tokenRequest(form url.Values) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// token calls /oauth/token as an OAuth client's backend would: no session, no CSRF token.
func (f oauthFlow) token(form url.Values) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	f.h.ServeHTTP(rr, tokenRequest(form))
	return rr
}

// tokenBasic is token with the client credentials in an HTTP Basic header,
// form-encoded first as RFC 6749 §2.3.1 requires.
func (f oauthFlow) tokenBasic(clientID, secret string, form url.Values) *httptest.ResponseRecorder {
	req := tokenRequest(form)
	req.SetBasicAuth(url.QueryEscape(clientID), url.QueryEscape(secret))
	rr := httptest.NewRecorder()
	f.h.ServeHTTP(rr, req)
	return rr
}

// assertTokenError checks for an RFC 6749 §5.2 error with nothing else in the body.
func assertTokenError(t *testing.T, rr *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if want := `{"error":"` + code + `"}`; rr.Code != status || strings.TrimSpace(rr.Body.String()) != want {
		t.Fatalf("want %d %s, got %d: %s", status, want, rr.Code, rr.Body.String())
	}
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
		{"another app's credentials", other.ClientID, otherSecret, testRedirectURI, http.StatusBadRequest},
		{"different redirect_uri", app.ClientID, secret, "https://client.example/other", http.StatusBadRequest},
		{"missing redirect_uri", app.ClientID, secret, "", http.StatusBadRequest},
		{"issuing app", app.ClientID, secret, testRedirectURI, http.StatusOK},
		{"replayed code", app.ClientID, secret, testRedirectURI, http.StatusBadRequest},
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
			if tt.want != http.StatusOK {
				assertTokenError(t, rr, tt.want, "invalid_grant")
				return
			}
			if rr.Code != tt.want {
				t.Fatalf("want %d, got %d: %s", tt.want, rr.Code, rr.Body.String())
			}
			var resp struct {
				AccessToken string `json:"access_token"`
			}
			if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil || resp.AccessToken == "" {
				t.Errorf("want an access_token, got %s (%v)", rr.Body.String(), err)
			}
			// RFC 6749 §5.1: a response carrying a token must not be cached.
			if got := rr.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", got)
			}
		})
	}
}

// RFC 6749 §5.2: every client-authentication failure gets the same answer, so the
// endpoint doesn't reveal which client_ids exist.
func TestOAuthTokenEndpoint_BadClientCredentials(t *testing.T) {
	f := newOAuthFlow(t)
	app, secret := f.createApp(t, testRedirectURI)
	code := location(t, f.confirm(app.ClientID, testRedirectURI, "", "approve")).Query().Get("code")
	grant := func() url.Values {
		return url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {testRedirectURI}}
	}

	tests := []struct {
		name     string
		basic    bool
		clientID string
		secret   string
	}{
		{"unknown client_id", false, "no-such-client", secret},
		{"wrong client_secret", false, app.ClientID, "wrong"},
		{"no client credentials", false, "", ""},
		{"unknown client_id via Basic", true, "no-such-client", secret},
		{"wrong client_secret via Basic", true, app.ClientID, "wrong"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rr *httptest.ResponseRecorder
			if tt.basic {
				rr = f.tokenBasic(tt.clientID, tt.secret, grant())
			} else {
				form := grant()
				form.Set("client_id", tt.clientID)
				form.Set("client_secret", tt.secret)
				rr = f.token(form)
			}
			assertTokenError(t, rr, http.StatusUnauthorized, "invalid_client")
			if challenge := rr.Header().Get("WWW-Authenticate"); tt.basic != strings.HasPrefix(challenge, "Basic ") {
				t.Errorf("WWW-Authenticate = %q, want a Basic challenge exactly when Basic was used", challenge)
			}
		})
	}

	// Client authentication fails before the code is touched, so it is still redeemable.
	if rr := f.tokenBasic(app.ClientID, secret, grant()); rr.Code != http.StatusOK {
		t.Errorf("issuing app via Basic: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// golang.org/x/oauth2 sends client credentials in a Basic header first (AuthStyleAutoDetect)
// and reads the error code from the JSON body.
func TestOAuthTokenEndpoint_OAuth2ClientAuthStyles(t *testing.T) {
	f := newOAuthFlow(t)
	srv := httptest.NewServer(f.h)
	t.Cleanup(srv.Close)
	app, secret := f.createApp(t, testRedirectURI)

	for _, style := range []oauth2.AuthStyle{oauth2.AuthStyleInHeader, oauth2.AuthStyleInParams} {
		cfg := oauth2.Config{
			ClientID:     app.ClientID,
			ClientSecret: secret,
			RedirectURL:  testRedirectURI,
			Endpoint:     oauth2.Endpoint{TokenURL: srv.URL + "/oauth/token", AuthStyle: style},
		}
		code := location(t, f.confirm(app.ClientID, testRedirectURI, "", "approve")).Query().Get("code")

		tok, err := cfg.Exchange(context.Background(), code)
		if err != nil || tok.AccessToken == "" {
			t.Fatalf("AuthStyle %d: Exchange = %v, %v; want an access token", style, tok, err)
		}
		var rErr *oauth2.RetrieveError
		if _, err := cfg.Exchange(context.Background(), code); !errors.As(err, &rErr) || rErr.ErrorCode != "invalid_grant" {
			t.Errorf("AuthStyle %d: replay = %v, want invalid_grant", style, err)
		}
	}
}
