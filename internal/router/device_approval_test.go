package router_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type deviceFlow struct {
	h       http.Handler
	svc     *service.Services
	db      *sql.DB
	userID  int64
	session string
	dc      *service.DeviceCode
}

func newDeviceFlow(t *testing.T, scopes ...string) deviceFlow {
	t.Helper()
	h, svc, db := newTestRouter(t)
	suffix := testutil.UniqueSuffix(t)
	uid, _ := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	return startDeviceGrant(t, h, svc, db, uid, "testpw_"+suffix, "mk-laptop", scopes...)
}

func startDeviceGrant(t *testing.T, h http.Handler, svc *service.Services, db *sql.DB, uid int64, username, device string, scopes ...string) deviceFlow {
	t.Helper()
	ip := deviceTestIP()
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM device_grants WHERE requester_ip = $1`, ip) })
	dc, err := svc.DeviceGrant.Create(context.Background(), ip, scopes, device)
	if err != nil {
		t.Fatal(err)
	}
	return deviceFlow{h, svc, db, uid, makeJWT(t, uid, username), dc}
}

func newReadWriteDeviceFlow(t *testing.T) deviceFlow {
	t.Helper()
	return newDeviceFlow(t, "repo:read", "repo:write")
}

// enter submits the code and returns the cookie that carries it to later requests.
func (f deviceFlow) enter(t *testing.T, code string) (*httptest.ResponseRecorder, *http.Cookie) {
	t.Helper()
	rec := serve(f.h, browserRequest(http.MethodPost, "/login/device", f.session, url.Values{"user_code": {code}}))
	for _, c := range rec.Result().Cookies() {
		if c.Name == "cz_device_code" && c.Value != "" {
			return rec, c
		}
	}
	return rec, nil
}

func (f deviceFlow) rawCode() string { return strings.ReplaceAll(f.dc.UserCode, "-", "") }

func (f deviceFlow) cookie(t *testing.T) *http.Cookie {
	t.Helper()
	_, c := f.enter(t, f.dc.UserCode)
	if c == nil {
		t.Fatal("entering the code set no cz_device_code cookie")
	}
	return c
}

func (f deviceFlow) approve(c *http.Cookie, form url.Values) *httptest.ResponseRecorder {
	req := browserRequest(http.MethodPost, "/login/device/approve", f.session, form)
	req.AddCookie(c)
	return serve(f.h, req)
}

func (f deviceFlow) status(t *testing.T) string {
	t.Helper()
	var s string
	if err := f.db.QueryRowContext(context.Background(), `SELECT status FROM device_grants WHERE user_code = $1`, f.rawCode()).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func (f deviceFlow) poll(t *testing.T) (*service.DeviceToken, error) {
	t.Helper()
	f.svc.DeviceGrant.SetClock(func() time.Time { return time.Now().Add(time.Minute) })
	t.Cleanup(func() { f.svc.DeviceGrant.SetClock(time.Now) })
	return f.svc.DeviceGrant.Poll(context.Background(), f.dc.DeviceCode)
}

func approveForm(password string, scopes ...string) url.Values {
	return url.Values{"password": {password}, "scope": scopes}
}

// assertNoAudit fails as soon as an audit row for action appears within a short window.
// The row is written by a goroutine, so absence can only be shown over a bounded wait.
func assertNoAudit(t *testing.T, db *sql.DB, action string, actorID int64) {
	t.Helper()
	for deadline := time.Now().Add(300 * time.Millisecond); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if n := countRows(t, db, `SELECT COUNT(*) FROM audit_log WHERE action = $1 AND actor_id = $2`, action, actorID); n != 0 {
			t.Fatalf("%d %s audit rows; want none", n, action)
		}
	}
}

// expireAfterLookups makes the grant look expired from the service's clock after n reads of it,
// which lands a request between the handler's lookup and the service's own.
func (f deviceFlow) expireAfterLookups(t *testing.T, n int) {
	t.Helper()
	var calls atomic.Int32
	f.svc.DeviceGrant.SetClock(func() time.Time {
		if int(calls.Add(1)) > n {
			return time.Now().Add(time.Hour)
		}
		return time.Now()
	})
	t.Cleanup(func() { f.svc.DeviceGrant.SetClock(time.Now) })
}

func clearsDeviceCookie(rec *httptest.ResponseRecorder) bool {
	for _, c := range rec.Result().Cookies() {
		if c.Name == "cz_device_code" && c.MaxAge < 0 {
			return true
		}
	}
	return false
}

func TestDeviceLogin_SignedOutVisitGoesToSignInAndIgnoresTheCode(t *testing.T) {
	f := newReadWriteDeviceFlow(t)

	rec := serve(f.h, httptest.NewRequest(http.MethodGet, "/login/device?user_code=BCDF-GHJK", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login?next=%2Flogin%2Fdevice" {
		t.Errorf("signed out = %d to %q; want 303 to /login?next=%%2Flogin%%2Fdevice", rec.Code, rec.Header().Get("Location"))
	}

	rec = serve(f.h, browserRequest(http.MethodGet, "/login/device?user_code="+f.dc.UserCode, f.session, nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || strings.Contains(body, f.dc.UserCode) || strings.Contains(body, "value=") {
		t.Errorf("signed in = %d; want 200 with the code from the query nowhere on the page", rec.Code)
	}
}

func TestDeviceLogin_EntryRejectsUnknownAndExpiredCodesAlike(t *testing.T) {
	f := newReadWriteDeviceFlow(t)
	testutil.Exec(t, f.db, `UPDATE device_grants SET expires_at = NOW() - interval '1 minute' WHERE user_code = $1`, f.rawCode())

	unknown, c1 := f.enter(t, "BCDF-GHJK")
	expired, c2 := f.enter(t, f.dc.UserCode)
	if unknown.Code != http.StatusBadRequest || expired.Code != http.StatusBadRequest {
		t.Fatalf("unknown = %d, expired = %d; want 400 for both", unknown.Code, expired.Code)
	}
	if c1 != nil || c2 != nil {
		t.Error("a rejected code must not set the cookie")
	}
	const msg = "That code isn&#39;t valid or has expired."
	if !strings.Contains(unknown.Body.String(), msg) || !strings.Contains(expired.Body.String(), msg) {
		t.Errorf("both should show the same message %q", msg)
	}
}

func TestDeviceLogin_EntryThenConfirmPage(t *testing.T) {
	f := newReadWriteDeviceFlow(t)

	typed := strings.ToLower(f.rawCode())
	rec, c := f.enter(t, typed)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login/device/confirm" || c == nil {
		t.Fatalf("entry = %d to %q, cookie %v; want 303 to /login/device/confirm with a cookie", rec.Code, rec.Header().Get("Location"), c)
	}
	if !c.HttpOnly || c.Path != "/login/device" {
		t.Errorf("cookie HttpOnly=%v Path=%q; want HttpOnly, /login/device", c.HttpOnly, c.Path)
	}

	req := browserRequest(http.MethodGet, "/login/device/confirm", f.session, nil)
	req.AddCookie(c)
	rec = serve(f.h, req)
	body := rec.Body.String()
	formatted := f.rawCode()[:4] + "-" + f.rawCode()[4:]
	if rec.Code != http.StatusOK || !strings.Contains(body, formatted) || !strings.Contains(body, "mk-laptop") {
		t.Fatalf("confirm = %d; want 200 showing %s and mk-laptop", rec.Code, formatted)
	}
	for _, s := range []string{"repo:read", "repo:write"} {
		if !strings.Contains(body, `value="`+s+`" checked`) {
			t.Errorf("scope %s is not ticked", s)
		}
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" ||
		!strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") ||
		rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("framing and cache headers = %v", rec.Header())
	}

	rec = serve(f.h, browserRequest(http.MethodGet, "/login/device/confirm", f.session, nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login/device" {
		t.Errorf("no cookie = %d to %q; want 303 to /login/device", rec.Code, rec.Header().Get("Location"))
	}
}

func TestDeviceLogin_ApproveNeedsConfirmation(t *testing.T) {
	f := newReadWriteDeviceFlow(t)
	c := f.cookie(t)

	for _, form := range []url.Values{approveForm("", "repo:read"), approveForm("wrong", "repo:read")} {
		if rec := f.approve(c, form); rec.Code != http.StatusForbidden {
			t.Errorf("password %q = %d; want 403", form.Get("password"), rec.Code)
		}
	}
	if _, err := f.svc.DeviceGrant.Lookup(context.Background(), f.dc.UserCode); err != nil {
		t.Errorf("the grant should still be pending: %v", err)
	}
	for range 3 {
		f.approve(c, approveForm("wrong", "repo:read"))
	}
	if rec := f.approve(c, approveForm("password1", "repo:read")); rec.Code != http.StatusTooManyRequests {
		t.Errorf("after five wrong attempts = %d; want 429", rec.Code)
	}
	if got := f.status(t); got != model.DeviceGrantPending {
		t.Errorf("status = %s; want pending", got)
	}

	g := newReadWriteDeviceFlow(t)
	rec := g.approve(g.cookie(t), approveForm("password1", "repo:read", "repo:write"))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Device authorized") {
		t.Fatalf("approve = %d; want 200 Device authorized", rec.Code)
	}
	if got := g.status(t); got != model.DeviceGrantApproved {
		t.Errorf("status = %s; want approved", got)
	}
	if !clearsDeviceCookie(rec) {
		t.Error("approving should clear the cz_device_code cookie")
	}
	takeAudit(t, g.db, model.AuditActionDeviceApprove, g.userID)
}

func TestDeviceLogin_ApprovedGrantYieldsAToken(t *testing.T) {
	f := newReadWriteDeviceFlow(t)
	if rec := f.approve(f.cookie(t), approveForm("password1", "repo:read", "repo:write")); rec.Code != http.StatusOK {
		t.Fatalf("approve = %d", rec.Code)
	}
	tok, err := f.poll(t)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tok.AccessToken, "czp_") || !slices.Equal(tok.Scopes, []string{"repo:read", "repo:write"}) || tok.UserID != f.userID {
		t.Errorf("token = %+v; want a czp_ token for both scopes", tok)
	}
	takeAudit(t, f.db, model.AuditActionDeviceApprove, f.userID)
}

func TestDeviceLogin_ScopesOnlyNarrow(t *testing.T) {
	t.Run("a subset", func(t *testing.T) {
		f := newReadWriteDeviceFlow(t)
		if rec := f.approve(f.cookie(t), approveForm("password1", "repo:read")); rec.Code != http.StatusOK {
			t.Fatalf("approve = %d", rec.Code)
		}
		tok, err := f.poll(t)
		if err != nil || !slices.Equal(tok.Scopes, []string{"repo:read"}) {
			t.Errorf("token = %+v, %v; want [repo:read]", tok, err)
		}
		takeAudit(t, f.db, model.AuditActionDeviceApprove, f.userID)
	})
	t.Run("forged scopes are dropped", func(t *testing.T) {
		f := newReadWriteDeviceFlow(t)
		if rec := f.approve(f.cookie(t), approveForm("password1", "repo:read", "repo:admin", "issues:write")); rec.Code != http.StatusOK {
			t.Fatalf("approve = %d", rec.Code)
		}
		tok, err := f.poll(t)
		if err != nil || !slices.Equal(tok.Scopes, []string{"repo:read"}) {
			t.Errorf("token = %+v, %v; want [repo:read]", tok, err)
		}
		takeAudit(t, f.db, model.AuditActionDeviceApprove, f.userID)
	})
	t.Run("nothing left to grant", func(t *testing.T) {
		for _, form := range []url.Values{approveForm("wrong", "repo:admin"), approveForm("wrong")} {
			f := newReadWriteDeviceFlow(t)
			rec := f.approve(f.cookie(t), form)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Keep at least one permission") {
				t.Errorf("scopes %v = %d; want 400 with the keep-one message", form["scope"], rec.Code)
			}
			if n := countRows(t, f.db, `SELECT reauth_failures FROM users WHERE id = $1`, f.userID); n != 0 {
				t.Errorf("reauth_failures = %d; the refusal must not spend an attempt", n)
			}
			if got := f.status(t); got != model.DeviceGrantPending {
				t.Errorf("status = %s; want pending", got)
			}
		}
	})
}

func TestDeviceLogin_Deny(t *testing.T) {
	f := newReadWriteDeviceFlow(t)
	rec := f.approve(f.cookie(t), url.Values{"action": {"deny"}})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Request denied") {
		t.Fatalf("deny = %d; want 200 Request denied", rec.Code)
	}
	if got := f.status(t); got != model.DeviceGrantDenied {
		t.Errorf("status = %s; want denied", got)
	}
	takeAudit(t, f.db, model.AuditActionDeviceDeny, f.userID)
	if _, err := f.poll(t); !errors.Is(err, service.ErrAccessDenied) {
		t.Errorf("poll err = %v; want ErrAccessDenied", err)
	}
}

func TestDeviceLogin_TwoFactor(t *testing.T) {
	f := newReadWriteDeviceFlow(t)
	testutil.EnableTOTP(t, f.db, f.userID)
	c := f.cookie(t)

	if rec := f.approve(c, approveForm("password1", "repo:read")); rec.Code != http.StatusForbidden {
		t.Errorf("password alone = %d; want 403", rec.Code)
	}
	form := approveForm("password1", "repo:read")
	form.Set("code", testutil.TOTPCode(t, testutil.TestTOTPSecret))
	if rec := f.approve(c, form); rec.Code != http.StatusOK {
		t.Errorf("password and code = %d; want 200", rec.Code)
	}
	takeAudit(t, f.db, model.AuditActionDeviceApprove, f.userID)
}

func TestDeviceLogin_AccountWithNoWayToConfirmIsRefused(t *testing.T) {
	f := newReadWriteDeviceFlow(t)
	testutil.Exec(t, f.db, `UPDATE users SET password_hash = '', email = '' WHERE id = $1`, f.userID)

	rec := f.approve(f.cookie(t), approveForm("password1", "repo:read"))
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "no way to confirm it") {
		t.Errorf("approve = %d; want 403 with the reauth_unavailable message", rec.Code)
	}
	if got := f.status(t); got != model.DeviceGrantPending {
		t.Errorf("status = %s; want pending", got)
	}
}

func TestDeviceLogin_EntryIsLimitedPerUser(t *testing.T) {
	f := newReadWriteDeviceFlow(t)
	for i := 1; i <= 50; i++ {
		if rec, _ := f.enter(t, "BCDF-GHJK"); rec.Code != http.StatusBadRequest {
			t.Fatalf("attempt %d = %d; want 400", i, rec.Code)
		}
	}
	rec, _ := f.enter(t, "BCDF-GHJK")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("attempt 51 = %d, Retry-After %q; want 429 with Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}

	other := newReadWriteDeviceFlow(t)
	if rec, _ := other.enter(t, "BCDF-GHJK"); rec.Code != http.StatusBadRequest {
		t.Errorf("another user = %d; want 400", rec.Code)
	}
}

func TestDeviceLogin_TokensCannotUseThePages(t *testing.T) {
	f := newReadWriteDeviceFlow(t)
	raw, _, err := f.svc.AccessToken.Generate(context.Background(), f.userID, "t", []string{"repo:write"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/login/device"},
		{http.MethodPost, "/login/device"},
		{http.MethodGet, "/login/device/confirm"},
		{http.MethodGet, "/login/device/approve"},
		{http.MethodPost, "/login/device/approve"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("Authorization", "Bearer "+raw)
		if rec := serve(f.h, req); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s with a token = %d; want 403", tc.method, tc.path, rec.Code)
		}
	}
}

func TestDeviceLogin_DenyLosesRaceToApproval(t *testing.T) {
	f := newReadWriteDeviceFlow(t)
	c := f.cookie(t)
	if err := f.svc.DeviceGrant.Approve(context.Background(), f.dc.UserCode, f.userID, []string{"repo:read"}); err != nil {
		t.Fatal(err)
	}

	rec := f.approve(c, url.Values{"action": {"deny"}})
	if rec.Code != http.StatusGone || !strings.Contains(rec.Body.String(), "expired or was already answered") || !clearsDeviceCookie(rec) {
		t.Errorf("deny after approval = %d; want 410 with the answered message and a cleared cookie", rec.Code)
	}
	assertNoAudit(t, f.db, model.AuditActionDeviceDeny, f.userID)
	if got := f.status(t); got != model.DeviceGrantApproved {
		t.Errorf("status = %s; want approved", got)
	}
}

func TestDeviceLogin_ApproveAfterAnswerGoesBackToEntry(t *testing.T) {
	f := newReadWriteDeviceFlow(t)
	c := f.cookie(t)
	if err := f.svc.DeviceGrant.Deny(context.Background(), f.dc.UserCode, f.userID); err != nil {
		t.Fatal(err)
	}

	rec := f.approve(c, approveForm("password1", "repo:read"))
	if rec.Code != http.StatusGone || !strings.Contains(rec.Body.String(), "expired or was already answered") {
		t.Errorf("approve after deny = %d; want 410 with the answered message", rec.Code)
	}
	assertNoAudit(t, f.db, model.AuditActionDeviceApprove, f.userID)
	if got := f.status(t); got != model.DeviceGrantDenied {
		t.Errorf("status = %s; want denied", got)
	}
}

func TestDeviceLogin_LosingARaceInsideTheLookupWindowAnswers410(t *testing.T) {
	for _, tc := range []struct {
		name   string
		form   url.Values
		action string
	}{
		{"deny", url.Values{"action": {"deny"}}, model.AuditActionDeviceDeny},
		{"approve", approveForm("password1", "repo:read"), model.AuditActionDeviceApprove},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newReadWriteDeviceFlow(t)
			c := f.cookie(t)
			// Reads: the handler's lookup, then the service's lookup passes, then its write sees the expiry.
			f.expireAfterLookups(t, 2)

			rec := f.approve(c, tc.form)
			if rec.Code != http.StatusGone || !strings.Contains(rec.Body.String(), "expired or was already answered") || !clearsDeviceCookie(rec) {
				t.Errorf("%s = %d; want 410 with the answered message and a cleared cookie", tc.name, rec.Code)
			}
			assertNoAudit(t, f.db, tc.action, f.userID)
			if got := f.status(t); got != model.DeviceGrantPending {
				t.Errorf("status = %s; the lost race must change nothing", got)
			}
		})
	}
}

func TestDeviceLogin_ConfirmPageTellsAnExpiredOrAnsweredRequestApart(t *testing.T) {
	f := newReadWriteDeviceFlow(t)
	c := f.cookie(t)
	if err := f.svc.DeviceGrant.Deny(context.Background(), f.dc.UserCode, f.userID); err != nil {
		t.Fatal(err)
	}
	req := browserRequest(http.MethodGet, "/login/device/confirm", f.session, nil)
	req.AddCookie(c)
	rec := serve(f.h, req)
	if rec.Code != http.StatusGone || !strings.Contains(rec.Body.String(), "expired or was already answered") || !clearsDeviceCookie(rec) {
		t.Errorf("confirm after the request was answered = %d; want 410 with the answered message and a cleared cookie", rec.Code)
	}
}

func TestDeviceLogin_ConfirmPageShowsBothIPs(t *testing.T) {
	f := newReadWriteDeviceFlow(t)
	var ip string
	if err := f.db.QueryRowContext(context.Background(), `SELECT requester_ip FROM device_grants WHERE user_code = $1`, f.rawCode()).Scan(&ip); err != nil {
		t.Fatal(err)
	}
	req := browserRequest(http.MethodGet, "/login/device/confirm", f.session, nil)
	req.RemoteAddr = "198.51.100.9:5555"
	req.AddCookie(f.cookie(t))
	body := serve(f.h, req).Body.String()
	if !strings.Contains(body, ip) || !strings.Contains(body, "198.51.100.9") {
		t.Errorf("confirm page should show the requester %s and the viewer 198.51.100.9", ip)
	}
}

func TestDeviceLogin_CookieAttributes(t *testing.T) {
	f := newReadWriteDeviceFlow(t)
	c := f.cookie(t)
	if c.SameSite != http.SameSiteLaxMode || c.MaxAge != int(service.DeviceGrantTTL.Seconds()) || c.Secure {
		t.Errorf("cookie SameSite=%v MaxAge=%d Secure=%v; want Lax, %d, not Secure on a plain-HTTP test config", c.SameSite, c.MaxAge, c.Secure, int(service.DeviceGrantTTL.Seconds()))
	}
}

func TestDeviceLogin_SigningOutClearsTheCookie(t *testing.T) {
	f := newReadWriteDeviceFlow(t)
	rec := serve(f.h, browserRequest(http.MethodPost, "/api/auth/logout", f.session, url.Values{}))
	if !clearsDeviceCookie(rec) {
		t.Errorf("logout = %d; want it to clear cz_device_code", rec.Code)
	}
}

func TestDeviceLogin_FailedConfirmationKeepsUntickedScopesUnticked(t *testing.T) {
	f := newReadWriteDeviceFlow(t)
	c := f.cookie(t)

	rec := f.approve(c, approveForm("wrong", "repo:read"))
	body := rec.Body.String()
	if rec.Code != http.StatusForbidden || !strings.Contains(body, `value="repo:read" checked`) || strings.Contains(body, `value="repo:write" checked`) || !strings.Contains(body, `value="repo:write"`) {
		t.Fatalf("failed confirmation = %d; want 403 with repo:read ticked and repo:write offered unticked", rec.Code)
	}

	if rec := f.approve(c, approveForm("password1", "repo:read")); rec.Code != http.StatusOK {
		t.Fatalf("resubmit = %d; want 200", rec.Code)
	}
	tok, err := f.poll(t)
	if err != nil || !slices.Equal(tok.Scopes, []string{"repo:read"}) {
		t.Errorf("token = %+v, %v; want [repo:read]", tok, err)
	}
	takeAudit(t, f.db, model.AuditActionDeviceApprove, f.userID)
}

func TestDeviceLogin_ForgedOrForeignCookieActsLikeNone(t *testing.T) {
	f := newReadWriteDeviceFlow(t)
	signed := f.cookie(t)

	suffix := testutil.UniqueSuffix(t) + "b"
	otherID, _ := testutil.SeedUserWithPassword(t, f.db, suffix, "password1")
	other := makeJWT(t, otherID, "testpw_"+suffix)

	for _, tc := range []struct {
		name    string
		session string
		cookie  *http.Cookie
	}{
		{"an unsigned code", f.session, &http.Cookie{Name: "cz_device_code", Value: f.rawCode()}},
		{"garbage", f.session, &http.Cookie{Name: "cz_device_code", Value: "not.a.cookie"}},
		{"another user's cookie", other, signed},
	} {
		get := browserRequest(http.MethodGet, "/login/device/confirm", tc.session, nil)
		get.AddCookie(tc.cookie)
		post := browserRequest(http.MethodPost, "/login/device/approve", tc.session, url.Values{"action": {"deny"}})
		post.AddCookie(tc.cookie)
		for _, req := range []*http.Request{get, post} {
			rec := serve(f.h, req)
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login/device" {
				t.Errorf("%s: %s %s = %d to %q; want 303 to /login/device", tc.name, req.Method, req.URL.Path, rec.Code, rec.Header().Get("Location"))
			}
		}
		if got := f.status(t); got != model.DeviceGrantPending {
			t.Errorf("%s: status = %s; want pending", tc.name, got)
		}
	}
}

func TestDeviceLogin_ApproveRedirectsGetBackToConfirm(t *testing.T) {
	f := newReadWriteDeviceFlow(t)
	rec := serve(f.h, browserRequest(http.MethodGet, "/login/device/approve", f.session, nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login/device/confirm" {
		t.Errorf("signed in = %d to %q; want 303 to /login/device/confirm", rec.Code, rec.Header().Get("Location"))
	}
	rec = serve(f.h, httptest.NewRequest(http.MethodGet, "/login/device/approve", nil))
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/login") {
		t.Errorf("signed out = %d to %q; want a redirect to sign in", rec.Code, rec.Header().Get("Location"))
	}
}

func TestDeviceLogin_PasswordlessAccountConfirmsWithAnEmailedCode(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	h, svc, db := newVerificationRouter(t, smtp)
	suffix := testutil.UniqueSuffix(t)
	uid := testutil.SeedPasswordlessUser(t, db, suffix, "g_dev_"+suffix)
	f := startDeviceGrant(t, h, svc, db, uid, "testnopw_"+suffix, "mk-laptop", "repo:read")
	c := f.cookie(t)

	if rec := f.approve(c, url.Values{"scope": {"repo:read"}}); rec.Code != http.StatusForbidden {
		t.Fatalf("without a code = %d; want 403", rec.Code)
	}
	rec := serve(h, htmxRequest(browserRequest(http.MethodPost, "/settings/confirm-code", f.session, url.Values{})))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Code sent") {
		t.Fatalf("send code = %d %s", rec.Code, rec.Body)
	}
	code := box.NextTo(t, "testnopw_"+suffix+"@test.invalid").ConfirmationCode(t)

	rec = f.approve(c, url.Values{"scope": {"repo:read"}, "email_code": {code}})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Device authorized") {
		t.Fatalf("with the emailed code = %d; want 200 Device authorized", rec.Code)
	}
	if got := f.status(t); got != model.DeviceGrantApproved {
		t.Errorf("status = %s; want approved", got)
	}
	takeAudit(t, db, model.AuditActionDeviceApprove, uid)
}

func TestDeviceLogin_ApprovalMailsAnEscapedNotice(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	h, svc, db := newVerificationRouter(t, smtp)
	suffix := testutil.UniqueSuffix(t)
	uid, email := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	f := startDeviceGrant(t, h, svc, db, uid, "testpw_"+suffix, "<b>x</b>", "repo:read")

	if rec := f.approve(f.cookie(t), approveForm("password1", "repo:read")); rec.Code != http.StatusOK {
		t.Fatalf("approve = %d", rec.Code)
	}
	mail := box.NextTo(t, email)
	var ip string
	if err := db.QueryRowContext(context.Background(), `SELECT requester_ip FROM device_grants WHERE user_code = $1`, f.rawCode()).Scan(&ip); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mail.Data, "&lt;b&gt;x&lt;/b&gt;") || strings.Contains(mail.Data, "<b>x</b>") || !strings.Contains(mail.Data, ip) {
		t.Errorf("notice should carry the escaped device name and %s:\n%.800s", ip, mail.Data)
	}
	box.Empty(t, 300*time.Millisecond)
	takeAudit(t, db, model.AuditActionDeviceApprove, uid)
}

func TestDeviceLogin_AuditDetails(t *testing.T) {
	f := newReadWriteDeviceFlow(t)
	if rec := f.approve(f.cookie(t), approveForm("password1", "repo:read")); rec.Code != http.StatusOK {
		t.Fatalf("approve = %d", rec.Code)
	}
	var ip string
	if err := f.db.QueryRowContext(context.Background(), `SELECT requester_ip FROM device_grants WHERE user_code = $1`, f.rawCode()).Scan(&ip); err != nil {
		t.Fatal(err)
	}
	md := takeAuditMetadata(t, f.db, model.AuditActionDeviceApprove, f.userID)
	scopes, _ := md["scopes"].([]any)
	if md["device"] != "mk-laptop" || md["ip"] != ip || len(scopes) != 1 || scopes[0] != "repo:read" {
		t.Errorf("approve details = %v", md)
	}

	testutil.Exec(t, f.db, `UPDATE device_grants SET last_polled_at = $2 WHERE user_code = $1`, f.rawCode(), time.Now().Add(-time.Hour))
	rec := postDevice(f.h, "/api/auth/device/token", url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {f.dc.DeviceCode},
	}, ip)
	if rec.Code != http.StatusOK {
		t.Fatalf("poll = %d %s", rec.Code, rec.Body)
	}
	md = takeAuditMetadata(t, f.db, model.AuditActionTokenCreate, f.userID)
	if md["source"] != "device login" || !strings.Contains(fmt.Sprint(md["token"]), "mk-laptop") {
		t.Errorf("token details = %v", md)
	}
}

func TestDeviceLogin_LDAPAccountConfirmsWithItsDirectoryPassword(t *testing.T) {
	h, svc, db := newTestRouter(t)
	ctx := context.Background()
	host, port := testutil.FakeLDAP(t, "dirpass")
	prior, err := svc.SSO.GetConfig(ctx, "ldap")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if prior == nil {
			testutil.Exec(t, db, `DELETE FROM sso_configs WHERE provider = 'ldap'`)
		} else if err := svc.SSO.SetConfig(ctx, "ldap", prior.Config, prior.Enabled); err != nil {
			t.Errorf("restore ldap config: %v", err)
		}
	})
	if err := svc.SSO.SetConfig(ctx, "ldap", map[string]string{
		model.LDAPKeyHost: host, model.LDAPKeyPort: port, model.LDAPKeyBindDNTmpl: "uid=%s,ou=people,dc=test",
	}, true); err != nil {
		t.Fatal(err)
	}

	suffix := testutil.UniqueSuffix(t)
	username := "testldap_" + suffix
	var uid int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, sso_provider, sso_id, is_invited)
		 VALUES ($1, $2, '', 'ldap', $3, true) RETURNING id`,
		username, username+"@ldap.local", "uid="+username+",ou=people,dc=test",
	).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testutil.DeleteUsers(t, db, uid) })
	f := startDeviceGrant(t, h, svc, db, uid, username, "mk-laptop", "repo:read")
	c := f.cookie(t)

	req := browserRequest(http.MethodGet, "/login/device/confirm", f.session, nil)
	req.AddCookie(c)
	if body := serve(h, req).Body.String(); !strings.Contains(body, "Directory password") || strings.Contains(body, "Email me a code") {
		t.Error("an LDAP account should be asked for its directory password and not offered an emailed code")
	}

	if rec := f.approve(c, approveForm("not-the-password", "repo:read")); rec.Code != http.StatusForbidden {
		t.Errorf("wrong directory password = %d; want 403", rec.Code)
	}
	if got := f.status(t); got != model.DeviceGrantPending {
		t.Errorf("status = %s; want pending", got)
	}
	rec := f.approve(c, approveForm("dirpass", "repo:read"))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Device authorized") {
		t.Fatalf("right directory password = %d; want 200 Device authorized", rec.Code)
	}
	if got := f.status(t); got != model.DeviceGrantApproved {
		t.Errorf("status = %s; want approved", got)
	}
	takeAudit(t, db, model.AuditActionDeviceApprove, uid)
}

// A real Google or SAML round trip needs the provider; these cover everything on our side of it:
// starting the sign-in from the device page, and the one-time code it leaves in cz_reauth.
func TestDeviceLogin_ProviderAccountConfirmsWithTheSignInCode(t *testing.T) {
	h, svc, db := newGoogleRouter(t, "client-123")
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	uid := testutil.SeedPasswordlessUser(t, db, suffix, "g_dev_"+suffix)
	otherID := testutil.SeedPasswordlessUser(t, db, suffix+"_o", "g_other_"+suffix)
	f := startDeviceGrant(t, h, svc, db, uid, "testnopw_"+suffix, "mk-laptop", "repo:read")
	c := f.cookie(t)

	signIn := func(userID int64, googleID string) *http.Cookie {
		t.Helper()
		state, _, err := svc.Reauth.BeginProviderSignIn(ctx, userID, "google")
		if err != nil {
			t.Fatal(err)
		}
		gotID, code, err := svc.Reauth.FinishGoogleSignIn(ctx, state, googleID)
		if err != nil || gotID != userID {
			t.Fatalf("FinishGoogleSignIn = %d, %v", gotID, err)
		}
		return &http.Cookie{Name: "cz_reauth", Value: code}
	}
	confirmPage := func(extra *http.Cookie) string {
		req := browserRequest(http.MethodGet, "/login/device/confirm", f.session, nil)
		req.AddCookie(c)
		if extra != nil {
			req.AddCookie(extra)
		}
		return serve(h, req).Body.String()
	}
	approveWith := func(code *http.Cookie) *httptest.ResponseRecorder {
		req := browserRequest(http.MethodPost, "/login/device/approve", f.session, url.Values{"scope": {"repo:read"}})
		req.AddCookie(c)
		req.AddCookie(code)
		return serve(h, req)
	}

	start := browserRequest(http.MethodPost, "/settings/reauth/google", f.session, url.Values{"return_to": {"/login/device/confirm"}})
	rec := serve(h, start)
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || !strings.HasPrefix(loc, "https://accounts.google.com/") {
		t.Fatalf("start sign-in = %d to %q; want 303 to Google", rec.Code, loc)
	}
	if ret := setCookie(rec, "cz_reauth_return"); ret == nil || ret.Value != "/login/device/confirm" {
		t.Errorf("cz_reauth_return = %+v; want the confirm page", ret)
	}

	before := confirmPage(nil)
	if !strings.Contains(before, "Confirm with Google") || !strings.Contains(before, "disabled") {
		t.Error("before signing in, the page offers Google and Authorize is disabled")
	}
	if rec := f.approve(c, url.Values{"scope": {"repo:read"}}); rec.Code != http.StatusForbidden {
		t.Errorf("without a code = %d; want 403", rec.Code)
	}

	if rec := approveWith(signIn(otherID, "g_other_"+suffix)); rec.Code != http.StatusForbidden {
		t.Errorf("another account's sign-in code = %d; want 403", rec.Code)
	}
	if got := f.status(t); got != model.DeviceGrantPending {
		t.Errorf("status = %s; want pending", got)
	}

	mine := signIn(uid, "g_dev_"+suffix)
	if after := confirmPage(mine); !strings.Contains(after, "Confirmed by signing in again with Google") {
		t.Error("after signing in, the page says so")
	}
	if rec := approveWith(mine); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Device authorized") {
		t.Fatalf("own sign-in code = %d; want 200 Device authorized", rec.Code)
	}
	if got := f.status(t); got != model.DeviceGrantApproved {
		t.Errorf("status = %s; want approved", got)
	}
	takeAudit(t, db, model.AuditActionDeviceApprove, uid)

	g := startDeviceGrant(t, h, svc, db, uid, "testnopw_"+suffix, "second", "repo:read")
	req := browserRequest(http.MethodPost, "/login/device/approve", g.session, url.Values{"scope": {"repo:read"}})
	req.AddCookie(g.cookie(t))
	req.AddCookie(mine)
	if rec := serve(h, req); rec.Code != http.StatusForbidden {
		t.Errorf("the spent code again = %d; want 403", rec.Code)
	}
}

func TestDeviceLogin_NoticeNamesAnUnnamedDeviceAndFlagsItUnverified(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	h, svc, db := newVerificationRouter(t, smtp)
	suffix := testutil.UniqueSuffix(t)
	uid, email := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	f := startDeviceGrant(t, h, svc, db, uid, "testpw_"+suffix, "", "repo:read")

	if rec := f.approve(f.cookie(t), approveForm("password1", "repo:read")); rec.Code != http.StatusOK {
		t.Fatalf("approve = %d", rec.Code)
	}
	mail := box.NextTo(t, email)
	if !strings.Contains(mail.Data, "cz CLI") || !strings.Contains(mail.Data, "name unverified") || !strings.Contains(mail.Data, "Access tokens") {
		t.Errorf("notice should name the default device, flag the name unverified and point to Access tokens:\n%.800s", mail.Data)
	}
	takeAudit(t, db, model.AuditActionDeviceApprove, uid)
}
