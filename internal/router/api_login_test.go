package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func postJSON(h http.Handler, path string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", testCSRF)
	return serve(h, req, &http.Cookie{Name: "csrf_token", Value: testCSRF})
}

func TestAPILogin_TOTPUserGetsNoSession(t *testing.T) {
	h, svc, db := newTestRouter(t)
	suffix := testutil.UniqueSuffix(t)
	const password = "api-login-password"
	userID, email := testutil.SeedUserWithPassword(t, db, suffix, password)
	secret, _, err := svc.TOTP.Generate("testpw_"+suffix, "Cloudzilla")
	if err != nil {
		t.Fatalf("TOTP.Generate: %v", err)
	}
	if _, err := svc.TOTP.Enable(context.Background(), userID, secret, totpNow(t, secret)); err != nil {
		t.Fatalf("TOTP.Enable: %v", err)
	}

	rr := postJSON(h, "/api/auth/login", map[string]string{"email": email, "password": password})
	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if rr.Code != http.StatusUnauthorized || body["error"] != "totp_required" {
		t.Fatalf("want 401 totp_required, got %d: %v", rr.Code, body)
	}
	if setsCookie(rr, "cz_token") || body["token"] != nil {
		t.Fatal("API login issued a session before the second factor was checked")
	}
}
