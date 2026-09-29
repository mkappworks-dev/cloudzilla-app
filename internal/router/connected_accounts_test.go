package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestConnectedAccounts_CookieSessionNeedsCSRFToken(t *testing.T) {
	h, _, db := newTestRouter(t)
	suffix := testutil.UniqueSuffix(t)
	userID, _ := testutil.SeedUserWithPassword(t, db, suffix, "router password")
	session := makeJWT(t, userID, "testpw_"+suffix)

	for _, path := range []string{"/settings/connected-accounts/google", "/settings/connected-accounts/google/disconnect"} {
		t.Run(path, func(t *testing.T) {
			post := func(csrfField string) *httptest.ResponseRecorder {
				form := url.Values{"password": {"router password"}}
				if csrfField != "" {
					form.Set("csrf_token", csrfField)
				}
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.AddCookie(&http.Cookie{Name: "cz_token", Value: session})
				req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "csrf-" + suffix})
				rr := httptest.NewRecorder()
				h.ServeHTTP(rr, req)
				return rr
			}

			if rr := post(""); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "CSRF") {
				t.Errorf("without a CSRF token: got %d %q, want 403 CSRF token mismatch", rr.Code, rr.Body.String())
			}
			if rr := post("csrf-" + suffix); rr.Code != http.StatusSeeOther || !strings.HasPrefix(rr.Header().Get("Location"), "/settings?profile_error=") {
				t.Errorf("with the CSRF token: got %d to %q, want the handler's 303 back to settings", rr.Code, rr.Header().Get("Location"))
			}
		})
	}
}

// Link mode reads the session, so the callback route must run optional auth.
func TestGoogleCallback_LinkModeSeesTheSession(t *testing.T) {
	h, svc, db := newTestRouter(t)
	suffix := testutil.UniqueSuffix(t)
	userID, _ := testutil.SeedUserWithPassword(t, db, suffix, "router password")
	state, _, err := svc.OAuthLink.BeginLink(context.Background(), userID, "router password", "")
	if err != nil {
		t.Fatalf("BeginLink: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?error=access_denied&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: "cz_token", Value: makeJWT(t, userID, "testpw_"+suffix)})
	req.AddCookie(&http.Cookie{Name: "oauth_link_state", Value: state})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if got := rr.Header().Get("Location"); !strings.Contains(got, "profile_error=google_link_cancelled") {
		t.Errorf("got %d to %q, want the signed-in cancel error", rr.Code, got)
	}
}
