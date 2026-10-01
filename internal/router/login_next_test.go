package router_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const testCSRF = "test-csrf-token"

var hiddenNextRE = regexp.MustCompile(`name="next" value="([^"]*)"`)

// hiddenNext returns the value of the form's hidden next input, failing the
// test if the page has none.
func hiddenNext(t *testing.T, body string) string {
	t.Helper()
	m := hiddenNextRE.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("page has no hidden next input:\n%s", body)
	}
	return html.UnescapeString(m[1])
}

func serve(h http.Handler, req *http.Request, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func postForm(h http.Handler, path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	form.Set("csrf_token", testCSRF)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return serve(h, req, append(cookies, &http.Cookie{Name: "csrf_token", Value: testCSRF})...)
}

func responseCookie(t *testing.T, rr *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range rr.Result().Cookies() {
		if c.Name == name && c.Value != "" {
			return c
		}
	}
	t.Fatalf("response sets no %s cookie", name)
	return nil
}

func TestLogin_ReturnsToOAuthConsent(t *testing.T) {
	h, svc, db := newTestRouter(t)
	suffix := testutil.UniqueSuffix(t)
	const password = "login-next-password"
	userID, email := testutil.SeedUserWithPassword(t, db, suffix, password)
	app, _, err := svc.OAuthApp.CreateApp(context.Background(), userID, "Login Next App", "", "", []string{"http://localhost/cb"})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	authorize := "/oauth/authorize?" + url.Values{
		"client_id":    {app.ClientID},
		"redirect_uri": {"http://localhost/cb"},
		"scope":        {model.ScopeRepoRead},
		"state":        {"xyz"},
	}.Encode()

	rr := serve(h, httptest.NewRequest(http.MethodGet, authorize, nil))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("signed-out authorize: want 303, got %d", rr.Code)
	}
	loginURL, err := url.Parse(rr.Header().Get("Location"))
	if err != nil || loginURL.Path != "/login" {
		t.Fatalf("signed-out authorize redirected to %q, want /login", rr.Header().Get("Location"))
	}
	if got := loginURL.Query().Get("next"); got != authorize {
		t.Fatalf("login next = %q, want %q", got, authorize)
	}

	rr = serve(h, httptest.NewRequest(http.MethodGet, loginURL.RequestURI(), nil))
	next := hiddenNext(t, rr.Body.String())

	rr = postForm(h, "/login", url.Values{"email": {email}, "password": {"wrong"}, "next": {next}})
	if got := hiddenNext(t, rr.Body.String()); got != next {
		t.Fatalf("after a failed login, hidden next = %q, want %q", got, next)
	}

	rr = postForm(h, "/login", url.Values{"email": {email}, "password": {password}, "next": {next}})
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != authorize {
		t.Fatalf("login: want 303 to %q, got %d to %q", authorize, rr.Code, rr.Header().Get("Location"))
	}

	rr = serve(h, httptest.NewRequest(http.MethodGet, authorize, nil), responseCookie(t, rr, "cz_token"))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Login Next App") {
		t.Fatalf("consent page after login: want 200 naming the app, got %d", rr.Code)
	}
}

func TestLogin_TOTPReturnsToNext(t *testing.T) {
	h, svc, db := newTestRouter(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	const password = "login-next-password"
	userID, email := testutil.SeedUserWithPassword(t, db, suffix, password)
	secret, _, err := svc.TOTP.Generate("testpw_"+suffix, "Cloudzilla")
	if err != nil {
		t.Fatalf("TOTP.Generate: %v", err)
	}
	if _, err := svc.TOTP.Enable(ctx, userID, secret, totpNow(t, secret)); err != nil {
		t.Fatalf("TOTP.Enable: %v", err)
	}
	const next = "/settings/security?tab=2fa"

	rr := postForm(h, "/login", url.Values{"email": {email}, "password": {password}, "next": {next}})
	twoFA, err := url.Parse(rr.Header().Get("Location"))
	if rr.Code != http.StatusSeeOther || err != nil || twoFA.Path != "/auth/2fa" {
		t.Fatalf("login: want 303 to /auth/2fa, got %d to %q", rr.Code, rr.Header().Get("Location"))
	}
	pending := responseCookie(t, rr, "cz_totp_pending")

	rr = serve(h, httptest.NewRequest(http.MethodGet, twoFA.RequestURI(), nil), pending)
	if got := hiddenNext(t, rr.Body.String()); got != next {
		t.Fatalf("2FA page hidden next = %q, want %q", got, next)
	}

	rr = postForm(h, "/auth/2fa/verify", url.Values{"code": {"000000"}, "next": {next}}, pending)
	if got := hiddenNext(t, rr.Body.String()); got != next {
		t.Fatalf("after a wrong code, hidden next = %q, want %q", got, next)
	}

	rr = postForm(h, "/auth/2fa/verify", url.Values{"code": {totpNow(t, secret)}, "next": {next}}, pending)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != next {
		t.Fatalf("2FA verify: want 303 to %q, got %d to %q", next, rr.Code, rr.Header().Get("Location"))
	}
}

func TestLogin_UnsafeNextGoesHome(t *testing.T) {
	h, _, db := newTestRouter(t)
	const password = "login-next-password"
	for _, next := range []string{"https://evil.test/", "//evil.test/", `/\evil.test/`, "/\t/evil.test/", "evil"} {
		t.Run(next, func(t *testing.T) {
			_, email := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), password)
			rr := postForm(h, "/login", url.Values{"email": {email}, "password": {password}, "next": {next}})
			if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/" {
				t.Fatalf("want 303 to /, got %d to %q", rr.Code, rr.Header().Get("Location"))
			}
		})
	}
}

// totpNow computes the current RFC 6238 code (SHA-1, 6 digits, 30s step), the
// parameters TOTPService advertises in its otpauth URL.
func totpNow(t *testing.T, secret string) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatalf("decode TOTP secret: %v", err)
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(time.Now().Unix()/30))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[off:])&0x7fffffff)%1_000_000)
}
