package router_test

// Integration tests for email verification through the real route table.
// All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/router"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newVerificationRouter(t *testing.T, smtp config.SMTPConfig) (http.Handler, *service.Services, *sql.DB) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	// Outlives the users a test deletes: with no account left, every route redirects to /setup.
	testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "http://localhost"},
		Auth:   config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: time.Hour, CookieName: "cz_token"},
		Git:    config.GitConfig{ReposRoot: t.TempDir()},
		SMTP:   smtp,
	}
	svc := service.New(store.New(db), cfg)
	h, err := router.New(svc, cfg, fstest.MapFS{})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	return h, svc, db
}

// browserRequest is what a browser sends: the session (if any) in a cookie and
// the double-submit CSRF token.
func browserRequest(method, target, session string, form url.Values) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if session != "" {
		req.AddCookie(&http.Cookie{Name: "cz_token", Value: session})
	}
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: testCSRF})
	req.Header.Set("X-CSRF-Token", testCSRF)
	return req
}

func superadminJWT(t *testing.T, userID int64, username string) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":           float64(userID),
		"username":      username,
		"is_superadmin": true,
		"exp":           float64(time.Now().Add(time.Hour).Unix()),
	})
	s, err := tok.SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("superadminJWT: %v", err)
	}
	return s
}

func isEmailVerified(t *testing.T, db *sql.DB, userID int64) bool {
	t.Helper()
	var verified bool
	if err := db.QueryRowContext(context.Background(), `SELECT email_verified_at IS NOT NULL FROM users WHERE id = $1`, userID).Scan(&verified); err != nil {
		t.Fatalf("read email_verified_at: %v", err)
	}
	return verified
}

// takeVerifyAudit waits for the goroutine-written audit row, removes it, and
// returns its actor and metadata.
func takeVerifyAudit(t *testing.T, db *sql.DB, targetID int64) (actorID int64, metadata map[string]any) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		var id int64
		var raw []byte
		err := db.QueryRowContext(context.Background(),
			`SELECT id, actor_id, metadata FROM audit_log WHERE action = $1 AND target_id = $2`,
			model.AuditActionEmailVerify, targetID).Scan(&id, &actorID, &raw)
		if err == nil {
			testutil.Exec(t, db, `DELETE FROM audit_log WHERE id = $1`, id)
			_ = json.Unmarshal(raw, &metadata)
			return actorID, metadata
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %s audit row for user %d: %v", model.AuditActionEmailVerify, targetID, err)
		}
	}
}

func TestEmailVerification_LinkFlowThroughRouter(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	h, _, db := newVerificationRouter(t, smtp)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	session := makeJWT(t, userID, "testuser_"+suffix)

	resend := browserRequest(http.MethodPost, "/settings/email/resend-verification", session, url.Values{})
	resend.Header.Set("HX-Request", "true")
	if rr := serve(h, resend); rr.Code != http.StatusNoContent {
		t.Fatalf("resend: got %d, want 204: %s", rr.Code, rr.Body.String())
	}
	token := box.Next(t).VerificationToken(t)

	// No session: the link has to work on whatever device opens the email.
	page := serve(h, browserRequest(http.MethodGet, "/verify-email?token="+token, "", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `name="token"`) {
		t.Fatalf("GET: got %d, want 200 with the confirm form: %.300s", page.Code, page.Body.String())
	}
	if !strings.Contains(page.Body.String(), "testuser_"+suffix) {
		t.Error("the confirm page doesn't name the account the address would be verified for")
	}
	if got := page.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q, want no-referrer", got)
	}
	if got := page.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if isEmailVerified(t, db, userID) {
		t.Fatal("GET verified the address; mail scanners would spend the link")
	}

	confirm := serve(h, browserRequest(http.MethodPost, "/verify-email", "", url.Values{"token": {token}}))
	if confirm.Code != http.StatusOK || !strings.Contains(confirm.Body.String(), "Email verified") {
		t.Fatalf("POST: got %d, want 200 and success: %.300s", confirm.Code, confirm.Body.String())
	}
	if !isEmailVerified(t, db, userID) {
		t.Fatal("address not verified")
	}
	if actor, meta := takeVerifyAudit(t, db, userID); actor != userID || meta["method"] != "link" {
		t.Errorf("audit actor %d, metadata %v; want the user, method link", actor, meta)
	}

	again := serve(h, browserRequest(http.MethodPost, "/verify-email", "", url.Values{"token": {token}}))
	if again.Code != http.StatusBadRequest || !strings.Contains(again.Body.String(), "isn&#39;t valid") {
		t.Errorf("reused link: got %d, want 400 invalid: %.300s", again.Code, again.Body.String())
	}
	if rr := serve(h, resend); rr.Code != http.StatusConflict {
		t.Errorf("resend once verified: got %d, want 409", rr.Code)
	}
}

func TestEmailVerification_POSTNeedsTheCSRFToken(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	h, svc, db := newVerificationRouter(t, smtp)
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	if err := svc.EmailVerifier.Send(context.Background(), userID); err != nil {
		t.Fatalf("Send: %v", err)
	}
	token := box.Next(t).VerificationToken(t)

	req := httptest.NewRequest(http.MethodPost, "/verify-email", strings.NewReader(url.Values{"token": {token}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if rr := serve(h, req); rr.Code != http.StatusForbidden {
		t.Errorf("POST without CSRF: got %d, want 403", rr.Code)
	}
	if isEmailVerified(t, db, userID) {
		t.Error("a cross-site POST verified the address")
	}
}

func TestEmailVerification_ExpiredAndUnknownLinkPages(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	h, svc, db := newVerificationRouter(t, smtp)
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	if err := svc.EmailVerifier.Send(context.Background(), userID); err != nil {
		t.Fatalf("Send: %v", err)
	}
	token := box.Next(t).VerificationToken(t)
	testutil.Exec(t, db, `UPDATE email_verification_tokens SET expires_at = NOW() - interval '1 second' WHERE user_id = $1`, userID)

	for _, tc := range []struct {
		name, token string
		want        int
		text        string
	}{
		{"expired", token, http.StatusGone, "expired"},
		{"unknown", strings.Repeat("A", len(token)), http.StatusBadRequest, "isn&#39;t valid"},
		{"missing", "", http.StatusBadRequest, "isn&#39;t valid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr := serve(h, browserRequest(http.MethodGet, "/verify-email?token="+tc.token, "", nil))
			if rr.Code != tc.want || !strings.Contains(rr.Body.String(), tc.text) || strings.Contains(rr.Body.String(), `name="token"`) {
				t.Errorf("got %d, want %d mentioning %q and no confirm form: %.300s", rr.Code, tc.want, tc.text, rr.Body.String())
			}
		})
	}
	if isEmailVerified(t, db, userID) {
		t.Error("an expired link verified the address")
	}
}

func TestEmailVerification_ResendCooldownThroughRouter(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	h, _, db := newVerificationRouter(t, smtp)
	suffix := testutil.UniqueSuffix(t)
	session := makeJWT(t, testutil.SeedUser(t, db, suffix), "testuser_"+suffix)

	send := func(htmx bool) *httptest.ResponseRecorder {
		req := browserRequest(http.MethodPost, "/settings/email/resend-verification", session, url.Values{})
		if htmx {
			req.Header.Set("HX-Request", "true")
		}
		return serve(h, req)
	}
	if rr := send(false); rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/settings#email" {
		t.Fatalf("first resend: got %d to %q", rr.Code, rr.Header().Get("Location"))
	}
	box.Next(t)
	if rr := send(true); rr.Code != http.StatusTooManyRequests || !strings.Contains(rr.Body.String(), "once a minute") {
		t.Errorf("HTMX resend within the cooldown: got %d %s, want 429", rr.Code, rr.Body.String())
	}
	if rr := send(false); rr.Code != http.StatusSeeOther || !strings.Contains(rr.Header().Get("Location"), "profile_error=verification_cooldown") {
		t.Errorf("form resend within the cooldown: got %d to %q", rr.Code, rr.Header().Get("Location"))
	}
	box.Empty(t, 200*time.Millisecond)
}

func TestEmailVerification_NewRoutesRefuseOAuthTokens(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	h, svc, db := newVerificationRouter(t, smtp)
	suffix := testutil.UniqueSuffix(t)
	adminID := testutil.SeedSuperadmin(t, db, suffix)
	targetID := testutil.SeedUser(t, db, suffix)
	all := []string{model.ScopeRepoRead, model.ScopeRepoWrite, model.ScopeIssuesWrite, model.ScopePullsWrite}
	adminTok := grantOAuthToken(t, svc, adminID, all...)
	userTok := grantOAuthToken(t, svc, targetID, all...)

	for _, tc := range []struct {
		name, token, path string
		form              url.Values
	}{
		{"resend", userTok, "/settings/email/resend-verification", url.Values{}},
		{"admin verify", adminTok, "/api/admin/users/verify-email", url.Values{"username": {"testuser_" + suffix}, "email": {"testuser_" + suffix + "@test.invalid"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Authorization", "Bearer "+tc.token)
			rr := serve(h, req)
			if rr.Code != http.StatusForbidden || !strings.Contains(rr.Header().Get("WWW-Authenticate"), "insufficient_scope") {
				t.Errorf("got %d (%q), want 403 insufficient_scope", rr.Code, rr.Header().Get("WWW-Authenticate"))
			}
		})
	}
	box.Empty(t, 200*time.Millisecond)
	if isEmailVerified(t, db, targetID) {
		t.Error("an OAuth token verified an address")
	}
}

func TestAdminVerifyEmail_SuperadminOnly(t *testing.T) {
	h, _, db := newVerificationRouter(t, config.SMTPConfig{})
	suffix := testutil.UniqueSuffix(t)
	adminID := testutil.SeedSuperadmin(t, db, suffix)
	targetID := testutil.SeedUser(t, db, suffix)
	otherSuffix := testutil.UniqueSuffix(t)
	otherID := testutil.SeedUser(t, db, otherSuffix)
	form := url.Values{"username": {"testuser_" + suffix}, "email": {"testuser_" + suffix + "@test.invalid"}}
	post := func(session string, form url.Values) *httptest.ResponseRecorder {
		req := browserRequest(http.MethodPost, "/api/admin/users/verify-email", session, form)
		req.Header.Set("HX-Request", "true")
		return serve(h, req)
	}

	if rr := post(makeJWT(t, otherID, "testuser_"+otherSuffix), form); rr.Code != http.StatusForbidden {
		t.Errorf("non-admin: got %d, want 403", rr.Code)
	}
	if rr := post(makeJWT(t, targetID, "testuser_"+suffix), form); rr.Code != http.StatusForbidden {
		t.Errorf("the user themself: got %d, want 403", rr.Code)
	}
	if isEmailVerified(t, db, targetID) {
		t.Fatal("a non-admin verified the address")
	}

	admin := superadminJWT(t, adminID, "testadmin_"+suffix)
	testutil.SetPassword(t, db, adminID, "admin-password")
	stale := url.Values{"username": form["username"], "email": {"old_" + suffix + "@test.invalid"}, "password": {"admin-password"}}
	if rr := post(admin, stale); rr.Code != http.StatusNotFound {
		t.Errorf("admin with a stale email: got %d, want 404", rr.Code)
	}
	form.Set("password", "admin-password")
	if rr := post(admin, form); rr.Code != http.StatusNoContent {
		t.Fatalf("admin: got %d, want 204: %s", rr.Code, rr.Body.String())
	}
	if !isEmailVerified(t, db, targetID) {
		t.Fatal("admin verification did not verify the address")
	}
	if actor, meta := takeVerifyAudit(t, db, targetID); actor != adminID || meta["method"] != "admin" || meta["email"] != form.Get("email") {
		t.Errorf("audit actor %d, metadata %v; want the admin, method admin, the email", actor, meta)
	}
}

func TestSettingsPage_ShowsEmailVerification(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	withSMTP, svc, db := newVerificationRouter(t, smtp)
	withoutSMTP, _, _ := newVerificationRouter(t, config.SMTPConfig{})
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	session := makeJWT(t, userID, "testuser_"+suffix)

	emailSection := func(h http.Handler) string {
		t.Helper()
		rr := serve(h, browserRequest(http.MethodGet, "/settings", session, nil))
		body := rr.Body.String()
		start := strings.Index(body, `<section id="email"`)
		if rr.Code != http.StatusOK || start < 0 {
			t.Fatalf("GET /settings: %d, email section found: %v", rr.Code, start >= 0)
		}
		return body[start : start+strings.Index(body[start:], "</section>")]
	}

	got := emailSection(withSMTP)
	if !strings.Contains(got, "Unverified") || !strings.Contains(got, "Resend verification email") || strings.Contains(got, "We emailed") {
		t.Errorf("unverified with SMTP, nothing sent: want the status and the resend button:\n%s", got)
	}
	if err := svc.EmailVerifier.Send(context.Background(), userID); err != nil {
		t.Fatalf("Send: %v", err)
	}
	box.Next(t)
	if got := emailSection(withSMTP); !strings.Contains(got, "We emailed a verification link") {
		t.Errorf("unverified with a link out: want it said:\n%s", got)
	}
	got = emailSection(withoutSMTP)
	if !strings.Contains(got, "Unverified") || strings.Contains(got, "Resend verification email") || !strings.Contains(got, "administrator hasn't configured") {
		t.Errorf("unverified without SMTP: want the status and the note, no button:\n%s", got)
	}
	testutil.Exec(t, db, `UPDATE users SET email_verified_at = NOW() WHERE id = $1`, userID)
	got = emailSection(withSMTP)
	if !strings.Contains(got, "Verified") || strings.Contains(got, "Unverified") || strings.Contains(got, "Resend verification email") {
		t.Errorf("verified: want the status only:\n%s", got)
	}
}
