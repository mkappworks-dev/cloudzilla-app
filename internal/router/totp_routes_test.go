package router_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const totpTestPassword = "totp-routes-password"

type totpEnv struct {
	h        http.Handler
	svc      *service.Services
	db       *sql.DB
	userID   int64
	email    string
	username string
	session  string
}

func newTOTPEnv(t *testing.T) totpEnv {
	t.Helper()
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	suffix := testutil.UniqueSuffix(t)
	id, email := testutil.SeedUserWithPassword(t, db, suffix, totpTestPassword)
	username := "testpw_" + suffix
	return totpEnv{h: h, svc: svc, db: db, userID: id, email: email, username: username, session: makeJWT(t, id, username)}
}

func (e totpEnv) post(target string, session string, form url.Values, htmx bool) *httptest.ResponseRecorder {
	req := browserRequest(http.MethodPost, target, session, form)
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	return serve(e.h, req)
}

func (e totpEnv) enabled(t *testing.T) bool {
	t.Helper()
	var v bool
	if err := e.db.QueryRow(`SELECT totp_enabled FROM users WHERE id = $1`, e.userID).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func (e totpEnv) pending(t *testing.T) *http.Cookie {
	t.Helper()
	tok, err := e.svc.TOTP.GeneratePendingToken(e.userID, testJWTSecret, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: "cz_totp_pending", Value: tok}
}

func (e totpEnv) verify(form url.Values, c *http.Cookie) *httptest.ResponseRecorder {
	req := browserRequest(http.MethodPost, "/auth/2fa/verify", "", form)
	if c != nil {
		req.AddCookie(c)
	}
	return serve(e.h, req)
}

func profileError(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	loc, err := url.Parse(rr.Header().Get("Location"))
	if err != nil {
		t.Fatalf("Location %q: %v", rr.Header().Get("Location"), err)
	}
	return loc.Query().Get("profile_error")
}

func cookieValue(rr *httptest.ResponseRecorder, name string) (string, bool) {
	for _, c := range rr.Result().Cookies() {
		if c.Name == name {
			return c.Value, c.MaxAge >= 0
		}
	}
	return "", false
}

func TestTOTPRoutes_SetupStoresASecretWithoutEnabling(t *testing.T) {
	e := newTOTPEnv(t)
	rr := e.post("/settings/security/setup", e.session, url.Values{}, false)
	wantStatus(t, rr, http.StatusSeeOther)
	if loc := rr.Header().Get("Location"); loc != "/settings#security" {
		t.Errorf("Location = %q", loc)
	}
	var secret sql.NullString
	if err := e.db.QueryRow(`SELECT totp_secret FROM users WHERE id = $1`, e.userID).Scan(&secret); err != nil {
		t.Fatal(err)
	}
	if secret.String == "" || e.enabled(t) {
		t.Errorf("secret = %q, enabled = %v; want a stored secret that is not yet enabled", secret.String, e.enabled(t))
	}
	if rr := e.post("/settings/security/setup", "", url.Values{}, false); rr.Code == http.StatusSeeOther && strings.HasPrefix(rr.Header().Get("Location"), "/settings") {
		t.Error("anonymous setup reached settings")
	}
}

func TestTOTPRoutes_EnableRefusalsLeaveTwoFactorOff(t *testing.T) {
	e := newTOTPEnv(t)
	secret := testutil.TestTOTPSecret
	good := testutil.TOTPCode(t, secret)

	cases := []struct {
		name string
		form url.Values
		want string
	}{
		{"missing code", url.Values{"secret": {secret}, "password": {totpTestPassword}}, "totp_missing_fields"},
		{"missing secret", url.Values{"code": {good}, "password": {totpTestPassword}}, "totp_missing_fields"},
		{"wrong password", url.Values{"secret": {secret}, "code": {good}, "password": {"nope"}}, "reauth_failed"},
		{"no password", url.Values{"secret": {secret}, "code": {good}}, "reauth_failed"},
		{"wrong code", url.Values{"secret": {secret}, "code": {"000000"}, "password": {totpTestPassword}}, "totp_invalid_code"},
	}
	for _, c := range cases {
		rr := e.post("/api/user/totp/enable", e.session, c.form, false)
		wantStatus(t, rr, http.StatusSeeOther)
		if got := profileError(t, rr); got != c.want {
			t.Errorf("%s: profile_error = %q, want %q", c.name, got, c.want)
		}
		if e.enabled(t) {
			t.Fatalf("%s: two-factor was enabled", c.name)
		}
	}

	rr := e.post("/api/user/totp/enable", e.session, url.Values{"secret": {secret}, "code": {"000000"}, "password": {totpTestPassword}}, true)
	wantStatus(t, rr, http.StatusOK)
	if rr.Header().Get("HX-Retarget") != "#totp-enable-form-error" {
		t.Errorf("HX-Retarget = %q", rr.Header().Get("HX-Retarget"))
	}
	wantStatus(t, e.post("/api/user/totp/enable", "", url.Values{"secret": {secret}}, false), http.StatusUnauthorized)
}

func TestTOTPRoutes_EnableThenDisable(t *testing.T) {
	e := newTOTPEnv(t)
	secret := testutil.TestTOTPSecret

	rr := e.post("/api/user/totp/enable", e.session, url.Values{"secret": {secret}, "code": {testutil.TOTPCode(t, secret)}, "password": {totpTestPassword}}, true)
	wantStatus(t, rr, http.StatusNoContent)
	if rr.Header().Get("HX-Redirect") != "/settings#security" {
		t.Errorf("HX-Redirect = %q", rr.Header().Get("HX-Redirect"))
	}
	codes, _ := cookieValue(rr, "cz_backup_codes")
	if n := len(strings.Split(codes, ",")); n != 10 {
		t.Errorf("backup codes cookie holds %d codes, want 10", n)
	}
	if !e.enabled(t) {
		t.Fatal("two-factor not enabled")
	}

	missing := e.post("/api/user/totp/disable", e.session, url.Values{"password": {totpTestPassword}}, false)
	wantStatus(t, missing, http.StatusSeeOther)
	if got := profileError(t, missing); got != "totp_missing_code" {
		t.Errorf("missing code: profile_error = %q", got)
	}
	good := testutil.TOTPCode(t, secret)
	wrongPassword := e.post("/api/user/totp/disable", e.session, url.Values{"code": {good}, "password": {"nope"}}, false)
	if got := profileError(t, wrongPassword); got != "reauth_failed" {
		t.Errorf("wrong password: profile_error = %q", got)
	}
	wrongCode := e.post("/api/user/totp/disable", e.session, url.Values{"code": {"000000"}, "password": {totpTestPassword}}, false)
	if got := profileError(t, wrongCode); got != "reauth_failed" {
		t.Errorf("wrong code: profile_error = %q; the confirmation step already checks the code", got)
	}
	if !e.enabled(t) {
		t.Fatal("a refused request disabled two-factor")
	}
	wantStatus(t, e.post("/api/user/totp/disable", "", url.Values{"code": {good}}, false), http.StatusUnauthorized)

	rr = e.post("/api/user/totp/disable", e.session, url.Values{"code": {good}, "password": {totpTestPassword}}, false)
	wantStatus(t, rr, http.StatusSeeOther)
	if e.enabled(t) {
		t.Error("two-factor still enabled")
	}
}

func TestTOTPRoutes_VerifyPageNeedsPendingCookie(t *testing.T) {
	e := newTOTPEnv(t)
	rr := serve(e.h, httptest.NewRequest(http.MethodGet, "/auth/2fa?next=/settings", nil))
	wantStatus(t, rr, http.StatusSeeOther)
	loc, _ := url.Parse(rr.Header().Get("Location"))
	if loc.Path != "/login" || loc.Query().Get("next") != "/settings" {
		t.Errorf("Location = %q", rr.Header().Get("Location"))
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/2fa?next=/settings", nil)
	req.AddCookie(e.pending(t))
	rr = serve(e.h, req)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "/auth/2fa/verify")
}

func TestTOTPRoutes_VerifyRejectsBadPendingTokens(t *testing.T) {
	e := newTOTPEnv(t)
	testutil.EnableTOTP(t, e.db, e.userID)
	code := testutil.TOTPCode(t, testutil.TestTOTPSecret)

	wantStatus(t, e.verify(url.Values{"code": {code}}, nil), http.StatusSeeOther)

	sign := func(key string, exp time.Time, sub any) *http.Cookie {
		tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": sub, "exp": exp.Unix()}).SignedString([]byte(key))
		if err != nil {
			t.Fatal(err)
		}
		return &http.Cookie{Name: "cz_totp_pending", Value: tok}
	}
	bad := map[string]*http.Cookie{
		"garbage":      {Name: "cz_totp_pending", Value: "not-a-jwt"},
		"wrong key":    sign("another-secret-entirely-different", time.Now().Add(time.Minute), float64(e.userID)),
		"expired":      sign(testJWTSecret, time.Now().Add(-time.Minute), float64(e.userID)),
		"unknown user": sign(testJWTSecret, time.Now().Add(time.Minute), float64(1<<40)),
	}
	for name, c := range bad {
		rr := e.verify(url.Values{"code": {code}, "next": {"/x"}}, c)
		wantStatus(t, rr, http.StatusSeeOther)
		if loc := rr.Header().Get("Location"); !strings.HasPrefix(loc, "/login") {
			t.Errorf("%s: Location = %q, want the login page", name, loc)
		}
		if _, ok := cookieValue(rr, "cz_token"); ok {
			t.Errorf("%s: a session cookie was issued", name)
		}
	}

	none := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{"sub": float64(e.userID), "exp": time.Now().Add(time.Minute).Unix()})
	s, err := none.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	rr := e.verify(url.Values{"code": {code}}, &http.Cookie{Name: "cz_totp_pending", Value: s})
	if _, ok := cookieValue(rr, "cz_token"); ok || !strings.HasPrefix(rr.Header().Get("Location"), "/login") {
		t.Errorf("an unsigned pending token was accepted: %d %q", rr.Code, rr.Header().Get("Location"))
	}
}

func TestTOTPRoutes_VerifyCodeStartsSessionAndClearsPending(t *testing.T) {
	e := newTOTPEnv(t)
	testutil.EnableTOTP(t, e.db, e.userID)

	rr := e.verify(url.Values{"code": {testutil.TOTPCode(t, testutil.TestTOTPSecret)}, "next": {"//evil.test/x"}}, e.pending(t))
	wantStatus(t, rr, http.StatusSeeOther)
	if loc := rr.Header().Get("Location"); loc != "/" {
		t.Errorf("Location = %q; a protocol-relative next must fall back to /", loc)
	}
	if v, _ := cookieValue(rr, "cz_token"); v == "" {
		t.Error("no session cookie")
	}
	if v, live := cookieValue(rr, "cz_totp_pending"); v != "" || live {
		t.Errorf("pending cookie not cleared: %q live=%v", v, live)
	}
}

func TestTOTPRoutes_VerifyWrongCodeRerendersWithError(t *testing.T) {
	e := newTOTPEnv(t)
	testutil.EnableTOTP(t, e.db, e.userID)
	rr := e.verify(url.Values{"code": {"000000"}, "next": {"/settings"}}, e.pending(t))
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "Invalid code. Please try again.")
	if _, ok := cookieValue(rr, "cz_token"); ok {
		t.Error("a session cookie was issued for a wrong code")
	}
	rr = e.verify(url.Values{"backup_code": {"deadbeef"}}, e.pending(t))
	bodyHas(t, rr, "Invalid code")
}

func TestTOTPRoutes_VerifyThrottlesAfterRepeatedFailures(t *testing.T) {
	e := newTOTPEnv(t)
	testutil.EnableTOTP(t, e.db, e.userID)
	pending := e.pending(t)
	for i := 0; i < 5; i++ {
		bodyHas(t, e.verify(url.Values{"code": {"000000"}}, pending), "Invalid code")
	}
	rr := e.verify(url.Values{"code": {testutil.TOTPCode(t, testutil.TestTOTPSecret)}}, pending)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "Too many incorrect codes")
	if _, ok := cookieValue(rr, "cz_token"); ok {
		t.Error("a correct code got a session while throttled")
	}
}

func TestTOTPRoutes_BackupCodeWorksOnce(t *testing.T) {
	e := newTOTPEnv(t)
	testutil.EnableTOTP(t, e.db, e.userID)
	raw, hashes, err := e.svc.TOTP.GenerateBackupCodes()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.NewUserStore(e.db).SetBackupCodes(context.Background(), e.userID, hashes); err != nil {
		t.Fatal(err)
	}

	rr := e.verify(url.Values{"backup_code": {" " + raw[0] + " "}}, e.pending(t))
	wantStatus(t, rr, http.StatusSeeOther)
	if v, _ := cookieValue(rr, "cz_token"); v == "" {
		t.Fatal("a valid backup code did not start a session")
	}
	rr = e.verify(url.Values{"backup_code": {raw[0]}}, e.pending(t))
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "Invalid code")
}

func TestTOTPRoutes_VerifyRefusesSuspendedAccount(t *testing.T) {
	e := newTOTPEnv(t)
	testutil.EnableTOTP(t, e.db, e.userID)
	testutil.Exec(t, e.db, `UPDATE users SET suspended_at = NOW() WHERE id = $1`, e.userID)
	rr := e.verify(url.Values{"code": {testutil.TOTPCode(t, testutil.TestTOTPSecret)}}, e.pending(t))
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "This account is suspended")
	if _, ok := cookieValue(rr, "cz_token"); ok {
		t.Error("a suspended account got a session")
	}
}
