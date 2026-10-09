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
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login/device" {
		t.Errorf("deny after approval = %d to %q; want 303 to /login/device", rec.Code, rec.Header().Get("Location"))
	}
	time.Sleep(200 * time.Millisecond)
	if n := countRows(t, f.db, `SELECT COUNT(*) FROM audit_log WHERE action = $1 AND actor_id = $2`, model.AuditActionDeviceDeny, f.userID); n != 0 {
		t.Errorf("%d deny audit rows; want none", n)
	}
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
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login/device" {
		t.Errorf("approve after deny = %d to %q; want 303 to /login/device", rec.Code, rec.Header().Get("Location"))
	}
	time.Sleep(200 * time.Millisecond)
	if n := countRows(t, f.db, `SELECT COUNT(*) FROM audit_log WHERE action = $1 AND actor_id = $2`, model.AuditActionDeviceApprove, f.userID); n != 0 {
		t.Errorf("%d approve audit rows; want none", n)
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
