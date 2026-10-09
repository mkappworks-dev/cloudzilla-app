package router_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type resetEdgeEnv struct {
	h      http.Handler
	svc    *service.Services
	db     *sql.DB
	userID int64
	email  string
	path   string
}

func newResetEdgeEnv(t *testing.T) resetEdgeEnv {
	t.Helper()
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	id, email := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")
	link, err := svc.PasswordReset.IssueLink(context.Background(), id, model.PasswordResetByAdmin)
	if err != nil {
		t.Fatal(err)
	}
	return resetEdgeEnv{h: h, svc: svc, db: db, userID: id, email: email, path: strings.TrimPrefix(link, "http://localhost")}
}

func (e resetEdgeEnv) submit(path string, form url.Values) *httptest.ResponseRecorder {
	return serve(e.h, browserRequest(http.MethodPost, path, "", form))
}

func (e resetEdgeEnv) canLogIn(password string) bool {
	rr := serve(e.h, browserRequest(http.MethodPost, "/login", "", url.Values{"email": {e.email}, "password": {password}}))
	return rr.Code == http.StatusSeeOther && rr.Header().Get("Location") != "/login"
}

func TestPasswordResetEdges_UnknownAndMalformedTokens(t *testing.T) {
	e := newResetEdgeEnv(t)
	for _, token := range []string{"unknown", strings.Repeat("a", 64), "%20"} {
		path := resetPath(token)
		wantStatus(t, serve(e.h, browserRequest(http.MethodGet, path, "", nil)), http.StatusBadRequest)
		rr := e.submit(path, url.Values{"password": {resetNewPassword}, "confirm": {resetNewPassword}})
		wantStatus(t, rr, http.StatusBadRequest)
		if rr.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("token %q: Cache-Control = %q", token, rr.Header().Get("Cache-Control"))
		}
	}
	if !e.canLogIn("password1") {
		t.Error("unknown tokens changed the real password")
	}
}

func TestPasswordResetEdges_PasswordLengthLimitsKeepTheLink(t *testing.T) {
	e := newResetEdgeEnv(t)

	short := e.submit(e.path, url.Values{"password": {"short"}, "confirm": {"short"}})
	wantStatus(t, short, http.StatusOK)
	bodyHas(t, short, "at least 8 characters")

	long := strings.Repeat("x", service.MaxPasswordBytes+1)
	rr := e.submit(e.path, url.Values{"password": {long}, "confirm": {long}})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "Password is too long")

	link, err := e.svc.PasswordReset.Check(context.Background(), e.path[strings.LastIndex(e.path, "/")+1:])
	if err != nil || link.State != model.PasswordResetPending {
		t.Fatalf("link after rejected passwords = %+v, %v; want still pending", link, err)
	}
	if !e.canLogIn("password1") {
		t.Error("a rejected reset changed the password")
	}
}

func TestPasswordResetEdges_LinkWorksOnceAndSecondPostIsRefused(t *testing.T) {
	e := newResetEdgeEnv(t)
	form := url.Values{"password": {resetNewPassword}, "confirm": {resetNewPassword}}
	wantStatus(t, e.submit(e.path, form), http.StatusSeeOther)
	takeAuditMetadata(t, e.db, model.AuditActionPasswordReset, e.userID)

	again := url.Values{"password": {"attacker-chosen-1"}, "confirm": {"attacker-chosen-1"}}
	wantStatus(t, e.submit(e.path, again), http.StatusBadRequest)
	if e.canLogIn("attacker-chosen-1") {
		t.Fatal("a spent link set a second password")
	}
	if !e.canLogIn(resetNewPassword) {
		t.Error("the first reset did not stick")
	}
}

func TestPasswordResetEdges_TwoFactorAccountNeedsACode(t *testing.T) {
	e := newResetEdgeEnv(t)
	testutil.EnableTOTP(t, e.db, e.userID)
	form := func(code string) url.Values {
		return url.Values{"password": {resetNewPassword}, "confirm": {resetNewPassword}, "code": {code}}
	}

	page := serve(e.h, browserRequest(http.MethodGet, e.path, "", nil))
	wantStatus(t, page, http.StatusOK)

	rr := e.submit(e.path, form(""))
	wantStatus(t, rr, http.StatusForbidden)
	bodyHas(t, rr, "two-factor code is incorrect")
	rr = e.submit(e.path, form("000000"))
	wantStatus(t, rr, http.StatusForbidden)
	if !e.canLogInWithoutTOTP("password1") {
		t.Error("a refused reset changed the password")
	}

	rr = e.submit(e.path, form(testutil.TOTPCode(t, testutil.TestTOTPSecret)))
	wantStatus(t, rr, http.StatusSeeOther)
	takeAuditMetadata(t, e.db, model.AuditActionPasswordReset, e.userID)
}

// canLogInWithoutTOTP is true when the password is accepted, even if sign-in then asks for the second factor.
func (e resetEdgeEnv) canLogInWithoutTOTP(password string) bool {
	rr := serve(e.h, browserRequest(http.MethodPost, "/login", "", url.Values{"email": {e.email}, "password": {password}}))
	return rr.Code == http.StatusSeeOther && strings.HasPrefix(rr.Header().Get("Location"), "/auth/2fa")
}

func TestPasswordResetEdges_RepeatedWrongCodesThrottle(t *testing.T) {
	e := newResetEdgeEnv(t)
	testutil.EnableTOTP(t, e.db, e.userID)
	bad := url.Values{"password": {resetNewPassword}, "confirm": {resetNewPassword}, "code": {"000000"}}
	for i := 0; i < 5; i++ {
		wantStatus(t, e.submit(e.path, bad), http.StatusForbidden)
	}
	good := url.Values{"password": {resetNewPassword}, "confirm": {resetNewPassword}, "code": {testutil.TOTPCode(t, testutil.TestTOTPSecret)}}
	rr := e.submit(e.path, good)
	wantStatus(t, rr, http.StatusTooManyRequests)
	bodyHas(t, rr, "Too many incorrect codes")
	if !e.canLogInWithoutTOTP("password1") {
		t.Error("a throttled reset changed the password")
	}
}

func TestPasswordResetEdges_ForgotPageRendersWithSMTP(t *testing.T) {
	smtp, _ := testutil.FakeSMTP(t)
	h, _, _ := newVerificationRouter(t, smtp)
	rr := serve(h, browserRequest(http.MethodGet, "/auth/password/forgot", "", nil))
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, `name="email"`)
}
