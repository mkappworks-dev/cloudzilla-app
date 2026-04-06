package handler_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mkappworks/cloudzilla/internal/config"
	"github.com/mkappworks/cloudzilla/internal/handler"
	"github.com/mkappworks/cloudzilla/internal/service"
	"github.com/mkappworks/cloudzilla/internal/store"
	"github.com/mkappworks/cloudzilla/internal/testutil"
)

const testJWTSecret = "test-handler-secret-32bytes-min!"
const testCookieName = "cz_token_test"

// newAuthHandler builds a minimal Handler with only the services needed by Login/Logout.
func newAuthHandler(db *sql.DB) *handler.Handler {
	stores := &store.Stores{
		User:        store.NewUserStore(db),
		SiteSetting: store.NewSiteSettingStore(db),
		AuditLog:    store.NewAuditLogStore(db),
	}
	cfg := &config.Config{
		Auth: config.AuthConfig{
			JWTSecret:  testJWTSecret,
			JWTExpiry:  24 * time.Hour,
			CookieName: testCookieName,
		},
	}
	svc := &service.Services{
		User:        service.NewUserService(stores.User, cfg.Auth),
		SiteSetting: service.NewSiteSettingService(stores.SiteSetting, stores.User),
		AuditLog:    service.NewAuditService(stores.AuditLog),
	}
	return handler.New(svc, cfg)
}

// loginBody serializes email and password into a JSON request body for POST /api/auth/login.
func loginBody(email, password string) *bytes.Buffer {
	b, _ := json.Marshal(map[string]string{"email": email, "password": password})
	return bytes.NewBuffer(b)
}

// --- Login tests ---

// TestLogin_ValidCredentials_200AndCookie verifies that valid credentials return HTTP 200,
// set an HttpOnly cookie containing the JWT, and include both "token" and "user" in the JSON body.
func TestLogin_ValidCredentials_200AndCookie(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	_, email := testutil.SeedUserWithPassword(t, db, suffix, "correctpassword")

	h := newAuthHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(email, "correctpassword"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.Login(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Cookie must be set with HttpOnly flag
	cookies := rr.Result().Cookies()
	var found bool
	for _, c := range cookies {
		if c.Name == testCookieName {
			found = true
			if !c.HttpOnly {
				t.Error("cookie must be HttpOnly")
			}
			if c.Value == "" {
				t.Error("cookie value must not be empty")
			}
		}
	}
	if !found {
		t.Errorf("expected cookie %q to be set", testCookieName)
	}

	// Response must contain token and user
	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["token"] == "" || body["token"] == nil {
		t.Error("response must contain token")
	}
	if body["user"] == nil {
		t.Error("response must contain user")
	}
}

// TestLogin_InvalidPassword_401 verifies that a correct email with the wrong password
// returns HTTP 401 (no information leakage about whether the email exists).
func TestLogin_InvalidPassword_401(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	_, email := testutil.SeedUserWithPassword(t, db, suffix, "correctpassword")

	h := newAuthHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(email, "wrongpassword"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.Login(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rr.Code)
	}
}

// TestLogin_UnknownEmail_401 verifies that an email that does not exist in the database
// returns HTTP 401 (same response as wrong password to prevent email enumeration).
func TestLogin_UnknownEmail_401(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newAuthHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		loginBody("nobody@test.invalid", "anypassword"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.Login(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rr.Code)
	}
}

// TestLogin_BadJSON_400 verifies that a malformed request body returns HTTP 400
// rather than a 500 or panic.
func TestLogin_BadJSON_400(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newAuthHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.Login(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rr.Code)
	}
}

// --- Logout tests ---

// TestLogout_ClearsCookie verifies that Logout returns HTTP 204 and sets the auth cookie
// to expired (MaxAge < 0) so the browser removes it.
func TestLogout_ClearsCookie(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newAuthHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: testCookieName, Value: "sometoken"})
	rr := httptest.NewRecorder()
	h.Logout(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("want 204, got %d", rr.Code)
	}

	// Cookie must be expired (MaxAge = -1 or past Expires)
	var found bool
	for _, c := range rr.Result().Cookies() {
		if c.Name == testCookieName {
			found = true
			if c.MaxAge >= 0 {
				t.Errorf("logout cookie MaxAge must be negative to clear, got %d", c.MaxAge)
			}
		}
	}
	if !found {
		t.Errorf("expected cookie %q in logout response", testCookieName)
	}
}

// TestLogout_NoExistingCookie_204 verifies that Logout returns HTTP 204 even when
// the request has no auth cookie (idempotent logout).
func TestLogout_NoExistingCookie_204(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newAuthHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	rr := httptest.NewRecorder()
	h.Logout(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Errorf("want 204, got %d", rr.Code)
	}
}
