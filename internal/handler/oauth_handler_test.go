package handler_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func fakeGoogle(t *testing.T, userinfo map[string]any) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			_, _ = io.WriteString(w, `{"access_token":"fake-token","token_type":"Bearer","expires_in":3600}`)
		case "/userinfo":
			_ = json.NewEncoder(w).Encode(userinfo)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	handler.UseFakeGoogle(t, srv.URL)
}

func googleCallback(t *testing.T, db *sql.DB) *httptest.ResponseRecorder {
	t.Helper()
	cfg := &config.Config{
		Auth:  config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: time.Hour, CookieName: testCookieName},
		OAuth: config.OAuthConfig{GoogleClientID: "test-client", GoogleClientSecret: "test-secret", GoogleRedirectURL: "http://localhost/auth/google/callback"},
	}
	h := handler.New(service.New(store.New(db), cfg), cfg)
	r := chi.NewRouter()
	r.Get("/auth/google/callback", h.GoogleOAuthCallback)

	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?state=s1&code=c1", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "s1"})
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func oauthIDOf(t *testing.T, db *sql.DB, userID int64) string {
	t.Helper()
	var oauthID string
	if err := db.QueryRowContext(context.Background(), `SELECT oauth_id FROM users WHERE id = $1`, userID).Scan(&oauthID); err != nil {
		t.Fatalf("read oauth_id: %v", err)
	}
	return oauthID
}

// The login audit row is written by a goroutine; wait for it so it is not orphaned after the user is deleted.
func deleteLoginAudit(t *testing.T, db *sql.DB, userID int64) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		var n int
		if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM audit_log WHERE actor_id = $1`, userID).Scan(&n); err != nil || n > 0 {
			break
		}
	}
	testutil.Exec(t, db, `DELETE FROM audit_log WHERE actor_id = $1`, userID)
}

func hasAuthCookie(rr *httptest.ResponseRecorder) bool {
	for _, c := range rr.Result().Cookies() {
		if c.Name == testCookieName && c.Value != "" {
			return true
		}
	}
	return false
}

func TestGoogleOAuthCallback_UnverifiedEmailDoesNotTakeOverAccount(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	victimID, victimEmail := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	fakeGoogle(t, map[string]any{"id": "g_attacker_" + suffix, "email": victimEmail, "verified_email": false, "name": "Mallory"})

	rr := googleCallback(t, db)

	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "verified") {
		t.Errorf("got %d, want 403 with the unverified-email login error; body: %.300s", rr.Code, rr.Body.String())
	}
	if hasAuthCookie(rr) {
		t.Error("an auth cookie was issued for an unverified email")
	}
	if got := oauthIDOf(t, db, victimID); got != "" {
		t.Errorf("victim's account was linked to Google ID %q", got)
	}
}

func TestGoogleOAuthCallback_VerifiedEmailLinksAccount(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID, email := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	t.Cleanup(func() { deleteLoginAudit(t, db, userID) })
	fakeGoogle(t, map[string]any{"id": "g_owner_" + suffix, "email": email, "verified_email": true, "name": "Owner"})

	rr := googleCallback(t, db)

	if rr.Code != http.StatusSeeOther || !hasAuthCookie(rr) {
		t.Fatalf("got %d (auth cookie: %v), want 303 with an auth cookie; body: %.300s", rr.Code, hasAuthCookie(rr), rr.Body.String())
	}
	if got := oauthIDOf(t, db, userID); got != "g_owner_"+suffix {
		t.Errorf("oauth_id = %q, want the Google ID linked", got)
	}
}
