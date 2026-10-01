package handler_test

// Integration tests for confirming with a fresh Google sign-in. They require
// TEST_DATABASE_DSN and skip otherwise.

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// startSignIn posts the confirm button for u and returns the state, with the
// cookies the browser now holds.
func (e *linkEnv) startSignIn(u linkUser, returnTo string) (string, []*http.Cookie) {
	e.t.Helper()
	rr := e.post("/settings/reauth/google", u.session, url.Values{"return_to": {returnTo}})
	loc, err := url.Parse(rr.Header().Get("Location"))
	if rr.Code != http.StatusSeeOther || err != nil || !strings.HasPrefix(loc.String(), e.google+"/auth") {
		e.t.Fatalf("start: got %d to %q, want 303 to the fake Google", rr.Code, rr.Header().Get("Location"))
	}
	if loc.Query().Get("max_age") != "0" {
		e.t.Errorf("auth URL max_age = %q, want 0 so Google authenticates again", loc.Query().Get("max_age"))
	}
	return loc.Query().Get("state"), rr.Result().Cookies()
}

func (e *linkEnv) signInCallback(session, state string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=c1&state="+url.QueryEscape(state), nil)
	req.AddCookie(&http.Cookie{Name: testCookieName, Value: session})
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

func (e *linkEnv) createToken(u linkUser, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/user/tokens", strings.NewReader(url.Values{"name": {"ci"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: testCookieName, Value: u.session})
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

// An account that signs in with Google confirms by signing in there again; the
// sign-in leaves a code that only this browser holds and that works once.
func TestProviderSignIn_GoogleConfirmsOneChangeInThisBrowser(t *testing.T) {
	e := newLinkEnv(t)
	googleID := "g_signin_" + testutil.UniqueSuffix(t)
	u := e.passwordlessUser(googleID)
	refused := func(rr *httptest.ResponseRecorder) bool {
		return strings.Contains(rr.Header().Get("Location"), "profile_error=reauth_failed")
	}

	if rr := e.createToken(u); !refused(rr) {
		t.Fatalf("before signing in again: got %d to %q, want the refusal", rr.Code, rr.Header().Get("Location"))
	}
	state, cookies := e.startSignIn(u, "/settings#tokens")
	e.googleAccount(googleID, u.email, true)
	rr := e.signInCallback(u.session, state, cookies)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/settings#tokens" {
		t.Fatalf("callback: got %d to %q, want back to /settings#tokens", rr.Code, rr.Header().Get("Location"))
	}
	code := responseCookie(rr, "cz_reauth")
	if code == nil || !code.HttpOnly {
		t.Fatalf("callback left no HttpOnly one-time code: %+v", code)
	}
	if rr := e.createToken(u, code); rr.Header().Get("Location") != "/settings#tokens" {
		t.Errorf("with the code: got %d to %q", rr.Code, rr.Header().Get("Location"))
	}
	if rr := e.createToken(u, code); !refused(rr) {
		t.Errorf("the same code again: got %d to %q, want the refusal", rr.Code, rr.Header().Get("Location"))
	}
}

// Signing in with some other Google account confirms nothing, and a state is
// good for one callback only.
func TestProviderSignIn_GoogleRefusesAnotherAccountAndReplays(t *testing.T) {
	e := newLinkEnv(t)
	googleID := "g_signin_" + testutil.UniqueSuffix(t)
	u := e.passwordlessUser(googleID)

	state, cookies := e.startSignIn(u, "/settings")
	e.googleAccount("g_someone_else_"+testutil.UniqueSuffix(t), "else@test.invalid", true)
	rr := e.signInCallback(u.session, state, cookies)
	if responseCookie(rr, "cz_reauth") != nil || responseCookie(rr, "cz_reauth_failed") == nil {
		t.Fatalf("another Google account: got cookies %v, want only the failure mark", rr.Result().Cookies())
	}
	e.googleAccount(googleID, u.email, true)
	if rr := e.signInCallback(u.session, state, cookies); responseCookie(rr, "cz_reauth") != nil {
		t.Error("a spent state confirmed on its second callback")
	}

	// Another browser, without the state cookie, can't finish a sign-in this one started.
	state, _ = e.startSignIn(u, "/settings")
	if rr := e.signInCallback(u.session, state, nil); responseCookie(rr, "cz_reauth") != nil {
		t.Error("a callback without the state cookie confirmed")
	}
}

// An account with a password confirms with it, not a provider sign-in.
func TestProviderSignIn_RefusedForPasswordAccounts(t *testing.T) {
	e := newLinkEnv(t)
	u := e.user()
	rr := e.post("/settings/reauth/google", u.session, url.Values{"return_to": {"/settings"}})
	if strings.HasPrefix(rr.Header().Get("Location"), e.google) || responseCookie(rr, "cz_reauth_failed") == nil {
		t.Errorf("password account: got %d to %q, want the failure mark and no trip to Google", rr.Code, rr.Header().Get("Location"))
	}
}
