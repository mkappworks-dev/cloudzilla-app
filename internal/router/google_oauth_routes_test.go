package router_test

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/router"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// newGoogleRouter has Google sign-in configured. A test that sends a code must
// first point the exchange at a fake with fakeGoogle.
func newGoogleRouter(t *testing.T, clientID string) (http.Handler, *service.Services, *sql.DB) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "http://localhost"},
		Auth:   config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: time.Hour, CookieName: "cz_token"},
		Git:    config.GitConfig{ReposRoot: t.TempDir()},
		OAuth: config.OAuthConfig{
			GoogleClientID: clientID, GoogleClientSecret: "secret", GoogleRedirectURL: "http://localhost/auth/google/callback",
		},
	}
	svc := service.New(store.New(db), cfg)
	h, err := router.New(svc, cfg, fstest.MapFS{})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	return h, svc, db
}

func setCookie(rr *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rr.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestGoogleOAuthRoutes_BeginNeedsConfiguration(t *testing.T) {
	h, _, _ := newGoogleRouter(t, "")
	rr := serve(h, httptest.NewRequest(http.MethodGet, "/auth/google", nil))
	wantStatus(t, rr, http.StatusNotImplemented)
	if setCookie(rr, "oauth_state") != nil {
		t.Error("an unconfigured begin set a state cookie")
	}
}

func TestGoogleOAuthRoutes_BeginRedirectsWithMatchingState(t *testing.T) {
	h, _, _ := newGoogleRouter(t, "client-123")
	rr := serve(h, httptest.NewRequest(http.MethodGet, "/auth/google?next="+url.QueryEscape("/org/repo?tab=x&y=1"), nil))
	wantStatus(t, rr, http.StatusTemporaryRedirect)

	loc, err := url.Parse(rr.Header().Get("Location"))
	if err != nil || loc.Host != "accounts.google.com" {
		t.Fatalf("Location = %q", rr.Header().Get("Location"))
	}
	q := loc.Query()
	if q.Get("client_id") != "client-123" || q.Get("redirect_uri") != "http://localhost/auth/google/callback" ||
		q.Get("response_type") != "code" || !strings.Contains(q.Get("scope"), "email") {
		t.Errorf("authorize params = %v", q)
	}
	state := setCookie(rr, "oauth_state")
	if state == nil || state.Value == "" || state.Value != q.Get("state") || !state.HttpOnly || state.Path != "/" {
		t.Fatalf("state cookie = %+v, state param = %q", state, q.Get("state"))
	}
	if len(state.Value) != 32 {
		t.Errorf("state %q is %d chars, want 32 hex chars", state.Value, len(state.Value))
	}
	next := setCookie(rr, "oauth_next")
	if next == nil {
		t.Fatal("no oauth_next cookie")
	}
	if got, _ := url.QueryUnescape(next.Value); got != "/org/repo?tab=x&y=1" {
		t.Errorf("oauth_next = %q", got)
	}

	again := serve(h, httptest.NewRequest(http.MethodGet, "/auth/google", nil))
	if s2 := setCookie(again, "oauth_state"); s2 == nil || s2.Value == state.Value {
		t.Error("two sign-ins shared a state")
	}
}

func TestGoogleOAuthRoutes_BeginWithoutNextClearsStaleReturnPath(t *testing.T) {
	h, _, _ := newGoogleRouter(t, "client-123")
	rr := serve(h, httptest.NewRequest(http.MethodGet, "/auth/google", nil))
	next := setCookie(rr, "oauth_next")
	if next == nil || next.MaxAge >= 0 {
		t.Errorf("oauth_next = %+v, want it expired", next)
	}
}

func TestGoogleOAuthRoutes_CallbackRefusesForeignState(t *testing.T) {
	h, _, _ := newGoogleRouter(t, "client-123")
	cases := map[string]*http.Request{
		"no cookie":         httptest.NewRequest(http.MethodGet, "/auth/google/callback?state=abc&code=c", nil),
		"mismatched cookie": httptest.NewRequest(http.MethodGet, "/auth/google/callback?state=abc&code=c", nil),
		"no state at all":   httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=c", nil),
	}
	cases["mismatched cookie"].AddCookie(&http.Cookie{Name: "oauth_state", Value: "other"})
	// Link and re-auth cookies only count when they hold this very state.
	cases["link cookie for another state"] = httptest.NewRequest(http.MethodGet, "/auth/google/callback?state=abc&code=c", nil)
	cases["link cookie for another state"].AddCookie(&http.Cookie{Name: "oauth_link_state", Value: "zzz"})
	cases["reauth cookie for another state"] = httptest.NewRequest(http.MethodGet, "/auth/google/callback?state=abc&code=c", nil)
	cases["reauth cookie for another state"].AddCookie(&http.Cookie{Name: "oauth_reauth_state", Value: "zzz"})

	for name, req := range cases {
		rr := serve(h, req)
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "invalid OAuth state") {
			t.Errorf("%s: %d %q, want 400 invalid OAuth state", name, rr.Code, rr.Body.String())
		}
		if setCookie(rr, "cz_token") != nil {
			t.Errorf("%s: a session cookie was issued", name)
		}
	}
}

func TestGoogleOAuthRoutes_ReauthRoundTripNeverSignsIn(t *testing.T) {
	h, _, db := newGoogleRouter(t, "client-123")
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedPasswordlessUser(t, db, suffix, "g-"+suffix)
	session := makeJWT(t, userID, "testnopw_"+suffix)

	start := browserRequest(http.MethodPost, "/settings/reauth/google", session, url.Values{"return_to": {"/settings#security"}})
	rr := serve(h, start)
	wantStatus(t, rr, http.StatusSeeOther)
	loc, _ := url.Parse(rr.Header().Get("Location"))
	if loc.Host != "accounts.google.com" || loc.Query().Get("prompt") != "select_account" || loc.Query().Get("max_age") != "0" {
		t.Fatalf("Location = %q, want a forced re-authentication", rr.Header().Get("Location"))
	}
	state := setCookie(rr, "oauth_reauth_state")
	if state == nil || state.Value != loc.Query().Get("state") || state.Path != "/auth/google/callback" || !state.HttpOnly {
		t.Fatalf("reauth cookie = %+v", state)
	}
	if ret := setCookie(rr, "cz_reauth_return"); ret != nil && ret.Value != "/settings#security" {
		t.Errorf("return cookie = %q", ret.Value)
	}

	// Google reporting an error is handled before any code is redeemed.
	cb := httptest.NewRequest(http.MethodGet, "/auth/google/callback?error=access_denied&state="+url.QueryEscape(state.Value), nil)
	cb.AddCookie(&http.Cookie{Name: "oauth_reauth_state", Value: state.Value})
	cb.AddCookie(&http.Cookie{Name: "cz_token", Value: session})
	rr = serve(h, cb)
	wantStatus(t, rr, http.StatusSeeOther)
	if setCookie(rr, "cz_token") != nil {
		t.Error("the re-auth callback issued a session")
	}
	if c := setCookie(rr, "oauth_reauth_state"); c == nil || c.MaxAge >= 0 {
		t.Errorf("reauth cookie not expired: %+v", c)
	}
	if c := setCookie(rr, "cz_reauth_failed"); c == nil || c.Value != "1" {
		t.Errorf("failure marker = %+v", c)
	}
	if got := rr.Header().Get("Location"); !strings.HasPrefix(got, "/settings") {
		t.Errorf("Location = %q", got)
	}
}

func TestGoogleOAuthRoutes_ReauthCallbackNeedsASignedInUser(t *testing.T) {
	h, _, _ := newGoogleRouter(t, "client-123")
	cb := httptest.NewRequest(http.MethodGet, "/auth/google/callback?state=s1&code=c", nil)
	cb.AddCookie(&http.Cookie{Name: "oauth_reauth_state", Value: "s1"})
	rr := serve(h, cb)
	wantStatus(t, rr, http.StatusSeeOther)
	if c := setCookie(rr, "cz_reauth_failed"); c == nil {
		t.Error("an anonymous re-auth callback was not marked failed")
	}
	if setCookie(rr, "cz_token") != nil {
		t.Error("an anonymous re-auth callback issued a session")
	}
}

func TestGoogleOAuthRoutes_ProviderSignInMismatchFails(t *testing.T) {
	h, _, db := newGoogleRouter(t, "client-123")
	suffix := testutil.UniqueSuffix(t)
	userID, _ := testutil.SeedUserWithPassword(t, db, suffix, "pw-for-mismatch")
	rr := serve(h, browserRequest(http.MethodPost, "/settings/reauth/google", makeJWT(t, userID, "testpw_"+suffix), url.Values{"return_to": {"//evil.test"}}))
	wantStatus(t, rr, http.StatusSeeOther)
	if setCookie(rr, "oauth_reauth_state") != nil {
		t.Error("a password-only account started a Google re-authentication")
	}
	if loc := rr.Header().Get("Location"); strings.Contains(loc, "evil.test") {
		t.Errorf("Location = %q; return_to must be a local path", loc)
	}
}

// fakeGoogle answers the token and userinfo calls and counts the token exchanges.
func fakeGoogle(t *testing.T, googleID string) *atomic.Int32 {
	t.Helper()
	var exchanges atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			exchanges.Add(1)
			_, _ = io.WriteString(w, `{"access_token":"fake-token","token_type":"Bearer","expires_in":3600}`)
		case "/userinfo":
			_, _ = fmt.Fprintf(w, `{"id":%q,"email":%q,"verified_email":true}`, googleID, googleID+"@example.test")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	handler.UseFakeGoogle(t, srv.URL)
	return &exchanges
}

func TestGoogleOAuthRoutes_CallbackRefusesEmptyOrMismatchedStateBeforeExchange(t *testing.T) {
	h, _, _ := newGoogleRouter(t, "client-123")
	exchanges := fakeGoogle(t, "g-"+testutil.UniqueSuffix(t))

	callback := func(query string, cookies ...*http.Cookie) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?"+query, nil)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		return req
	}
	cookie := func(name, value string) *http.Cookie { return &http.Cookie{Name: name, Value: value} }
	cases := map[string]*http.Request{
		"no cookie, no state":                  callback("code=c"),
		"no cookie, empty state":               callback("state=&code=c"),
		"empty cookie, no state":               callback("code=c", cookie("oauth_state", "")),
		"empty cookie, empty state":            callback("state=&code=c", cookie("oauth_state", "")),
		"empty cookie, state set":              callback("state=abc&code=c", cookie("oauth_state", "")),
		"mismatched cookie":                    callback("state=abc&code=c", cookie("oauth_state", "other")),
		"empty link cookie, empty state":       callback("state=&code=c", cookie("oauth_link_state", "")),
		"empty reauth cookie, empty state":     callback("state=&code=c", cookie("oauth_reauth_state", "")),
		"all three cookies empty, empty state": callback("state=&code=c", cookie("oauth_state", ""), cookie("oauth_link_state", ""), cookie("oauth_reauth_state", "")),
	}
	for name, req := range cases {
		rr := serve(h, req)
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "invalid OAuth state") {
			t.Errorf("%s: %d %q, want 400 invalid OAuth state", name, rr.Code, rr.Body.String())
		}
		if setCookie(rr, "cz_token") != nil {
			t.Errorf("%s: a session cookie was issued", name)
		}
	}
	if n := exchanges.Load(); n != 0 {
		t.Errorf("%d token exchanges, want 0: a refused callback must not reach Google", n)
	}
}

func TestGoogleOAuthRoutes_CallbackWithMatchingStateSignsIn(t *testing.T) {
	h, _, _ := newGoogleRouter(t, "client-123")
	exchanges := fakeGoogle(t, "g-"+testutil.UniqueSuffix(t))

	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?state=s1&code=c", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "s1"})
	rr := serve(h, req)
	wantStatus(t, rr, http.StatusSeeOther)
	if setCookie(rr, "cz_token") == nil {
		t.Error("a matching state issued no session cookie")
	}
	if n := exchanges.Load(); n != 1 {
		t.Errorf("%d token exchanges, want 1", n)
	}
}
