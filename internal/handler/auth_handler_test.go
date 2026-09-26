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

	"github.com/go-chi/chi/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
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

func assertAuthCookieCleared(t *testing.T, rr *httptest.ResponseRecorder) {
	t.Helper()
	for _, c := range rr.Result().Cookies() {
		if c.Name == testCookieName && c.MaxAge < 0 {
			return
		}
	}
	t.Error("auth cookie not cleared")
}

// The Sign out menu item is a plain form POST; a 204 would leave the browser
// on a page that still looks signed in.
func TestLogout_BrowserFormRedirectsHome(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newAuthHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", strings.NewReader("csrf_token=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: testCookieName, Value: "sometoken"})
	rr := httptest.NewRecorder()
	h.Logout(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/" {
		t.Errorf("Location = %q, want %q", loc, "/")
	}
	assertAuthCookieCleared(t, rr)
}

func TestLogout_HTMXFormPostGetsHXRedirect(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newAuthHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: testCookieName, Value: "sometoken"})
	rr := httptest.NewRecorder()
	h.Logout(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d", rr.Code)
	}
	if got := rr.Header().Get("HX-Redirect"); got != "/" {
		t.Errorf("HX-Redirect = %q, want %q", got, "/")
	}
	if loc := rr.Header().Get("Location"); loc != "" {
		t.Errorf("Location = %q, want none", loc)
	}
	assertAuthCookieCleared(t, rr)
}

// The Sign out form reaches Logout only if its script-injected csrf_token validates.
func TestLogout_FormPostThroughCSRF(t *testing.T) {
	db := testutil.OpenTestDB(t)
	r := chi.NewRouter()
	r.Use(middleware.CSRF(false))
	r.Post("/api/auth/logout", newAuthHandler(db).Logout)

	pageLoad := httptest.NewRecorder()
	r.ServeHTTP(pageLoad, httptest.NewRequest(http.MethodGet, "/", nil))
	var csrfToken string
	for _, c := range pageLoad.Result().Cookies() {
		if c.Name == "csrf_token" {
			csrfToken = c.Value
		}
	}
	if csrfToken == "" {
		t.Fatal("no csrf_token cookie issued")
	}

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrfToken})
		req.AddCookie(&http.Cookie{Name: testCookieName, Value: "sometoken"})
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		return rr
	}

	t.Run("valid token", func(t *testing.T) {
		rr := post("csrf_token=" + csrfToken)
		if rr.Code != http.StatusSeeOther {
			t.Fatalf("want 303, got %d: %s", rr.Code, rr.Body.String())
		}
		if loc := rr.Header().Get("Location"); loc != "/" {
			t.Errorf("Location = %q, want %q", loc, "/")
		}
		assertAuthCookieCleared(t, rr)
	})

	t.Run("missing token", func(t *testing.T) {
		rr := post("")
		if rr.Code != http.StatusForbidden {
			t.Fatalf("want 403, got %d", rr.Code)
		}
		for _, c := range rr.Result().Cookies() {
			if c.Name == testCookieName {
				t.Errorf("rejected request still touched the auth cookie: %+v", c)
			}
		}
	})
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
