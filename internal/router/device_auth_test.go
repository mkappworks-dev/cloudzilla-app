package router_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

var deviceIPCounter atomic.Int64

// deviceTestIP returns an IPv4 no other test in this run or a concurrent package run shares,
// so per-IP rate limits and live-grant counts never collide.
func deviceTestIP() string {
	n := deviceIPCounter.Add(1)
	return fmt.Sprintf("10.%d.%d.%d", os.Getpid()%250+1, n/250%250, n%250+1)
}

func postDevice(h http.Handler, path string, form url.Values, ip string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = ip + ":4444"
	return serve(h, req)
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("body %q is not JSON: %v", rec.Body.String(), err)
	}
	return m
}

func TestDeviceLogin_EndToEnd(t *testing.T) {
	h, svc, db := newTestRouter(t)
	uid := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	ip := deviceTestIP()
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM device_grants WHERE requester_ip = $1`, ip) })

	rec := postDevice(h, "/api/auth/device/code", url.Values{"scope": {"repo:read repo:write"}, "device_name": {"mk-laptop"}}, ip)
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("code = %d, Cache-Control %q; want 200, no-store", rec.Code, rec.Header().Get("Cache-Control"))
	}
	body := decode(t, rec)
	if body["verification_uri"] != "http://localhost/login/device" || body["expires_in"] != float64(900) || body["interval"] != float64(5) {
		t.Errorf("code body = %v", body)
	}
	if _, has := body["verification_uri_complete"]; has {
		t.Error("the response must not carry verification_uri_complete")
	}
	deviceCode, userCode := body["device_code"].(string), body["user_code"].(string)
	poll := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {deviceCode}}

	if rec := postDevice(h, "/api/auth/device/token", poll, ip); rec.Code != 400 || decode(t, rec)["error"] != "authorization_pending" {
		t.Errorf("pending poll = %d %s", rec.Code, rec.Body)
	}
	if rec := postDevice(h, "/api/auth/device/token", poll, ip); decode(t, rec)["error"] != "slow_down" {
		t.Errorf("early poll = %s; want slow_down", rec.Body)
	}

	if err := svc.DeviceGrant.Approve(context.Background(), userCode, uid, []string{"repo:read"}); err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, db, `UPDATE device_grants SET last_polled_at = $2 WHERE requester_ip = $1`, ip, time.Now().Add(-time.Hour))
	rec = postDevice(h, "/api/auth/device/token", poll, ip)
	tok := decode(t, rec)
	if rec.Code != 200 || tok["token_type"] != "bearer" || tok["scope"] != "repo:read" || !strings.HasPrefix(tok["access_token"].(string), "czp_") || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("token response = %d %s", rec.Code, rec.Body)
	}

	req := httptest.NewRequest("GET", "/api/user", nil)
	req.Header.Set("Authorization", "Bearer "+tok["access_token"].(string))
	if rec := serve(h, req); rec.Code != 200 {
		t.Errorf("GET /api/user with the issued token = %d; want 200", rec.Code)
	}

	if rec := postDevice(h, "/api/auth/device/token", poll, ip); rec.Code != 400 || decode(t, rec)["error"] != "invalid_grant" {
		t.Errorf("second redemption = %d %s; want invalid_grant", rec.Code, rec.Body)
	}
}

func TestDeviceLogin_Errors(t *testing.T) {
	h, _, db := newTestRouter(t)
	testutil.SeedUser(t, db, testutil.UniqueSuffix(t)) // the setup gate redirects every request until a user exists
	ip := deviceTestIP()
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM device_grants WHERE requester_ip = $1`, ip) })

	if rec := postDevice(h, "/api/auth/device/code", url.Values{"scope": {"repo:admin"}}, ip); rec.Code != 400 || decode(t, rec)["error"] != "invalid_scope" {
		t.Errorf("repo:admin = %d %s; want 400 invalid_scope", rec.Code, rec.Body)
	}
	if rec := postDevice(h, "/api/auth/device/token", url.Values{"grant_type": {"password"}, "device_code": {"x"}}, ip); decode(t, rec)["error"] != "unsupported_grant_type" {
		t.Errorf("wrong grant_type = %s", rec.Body)
	}
	if rec := postDevice(h, "/api/auth/device/token", url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}, ip); decode(t, rec)["error"] != "invalid_request" {
		t.Errorf("missing device_code = %s", rec.Body)
	}
	for i := 0; i < 5; i++ {
		postDevice(h, "/api/auth/device/code", url.Values{}, ip)
	}
	if rec := postDevice(h, "/api/auth/device/code", url.Values{}, ip); rec.Code != 429 {
		t.Errorf("sixth live grant = %d; want 429", rec.Code)
	}
}

func TestDeviceLogin_NoCSRFToken(t *testing.T) {
	h, _, db := newTestRouter(t)
	testutil.SeedUser(t, db, testutil.UniqueSuffix(t)) // the setup gate redirects every request until a user exists
	ip := deviceTestIP()
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM device_grants WHERE requester_ip = $1`, ip) })
	if rec := postDevice(h, "/api/auth/device/code", url.Values{}, ip); rec.Code != 200 {
		t.Errorf("code without a CSRF token = %d; want 200", rec.Code)
	}
}
