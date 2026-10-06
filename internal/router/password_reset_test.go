package router_test

// Integration tests for password reset through the real route table.
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
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const resetNewPassword = "new-password-1"

func resetPath(token string) string { return "/auth/password/reset/" + token }

// takeAuditMetadata waits for the goroutine-written audit row for action and targetID,
// removes it, and returns its metadata.
func takeAuditMetadata(t *testing.T, db *sql.DB, action string, targetID int64) map[string]any {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		var id int64
		var raw []byte
		err := db.QueryRowContext(context.Background(),
			`SELECT id, metadata FROM audit_log WHERE action = $1 AND target_id = $2`, action, targetID).Scan(&id, &raw)
		if err == nil {
			testutil.Exec(t, db, `DELETE FROM audit_log WHERE id = $1`, id)
			var metadata map[string]any
			_ = json.Unmarshal(raw, &metadata)
			return metadata
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %s audit row for user %d: %v", action, targetID, err)
		}
	}
}

func TestPasswordReset_LoginLinkNeedsSMTP(t *testing.T) {
	smtp, _ := testutil.FakeSMTP(t)
	withSMTP, _, _ := newVerificationRouter(t, smtp)
	if rr := serve(withSMTP, browserRequest(http.MethodGet, "/login", "", nil)); !strings.Contains(rr.Body.String(), `href="/auth/password/forgot"`) {
		t.Error("with SMTP, /login has no forgot-password link")
	}

	without, _, db := newVerificationRouter(t, config.SMTPConfig{})
	if rr := serve(without, browserRequest(http.MethodGet, "/login", "", nil)); strings.Contains(rr.Body.String(), `href="/auth/password/forgot"`) {
		t.Error("without SMTP, /login links to forgot-password")
	}
	_, email := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")
	for _, req := range []*http.Request{
		browserRequest(http.MethodGet, "/auth/password/forgot", "", nil),
		browserRequest(http.MethodPost, "/auth/password/forgot", "", url.Values{"email": {email}}),
	} {
		rr := serve(without, req)
		if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Ask an administrator") {
			t.Errorf("%s forgot without SMTP: %d, want the ask-an-administrator page", req.Method, rr.Code)
		}
	}
}

func TestPasswordReset_FlowThroughRouter(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	h, svc, db := newVerificationRouter(t, smtp)
	suffix := testutil.UniqueSuffix(t)
	userID, email := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	username := "testpw_" + suffix

	signIn := serve(h, browserRequest(http.MethodPost, "/login", "", url.Values{"email": {email}, "password": {"password1"}}))
	var session string
	for _, c := range signIn.Result().Cookies() {
		if c.Name == "cz_token" {
			session = c.Value
		}
	}
	if session == "" {
		t.Fatalf("sign-in: %d, no session cookie", signIn.Code)
	}
	repoID := testutil.SeedRepo(t, db, userID, username, suffix)
	testutil.Exec(t, db, `UPDATE repositories SET private = TRUE WHERE id = $1`, repoID)
	pat, _, err := svc.AccessToken.Create(context.Background(), userID, service.NewToken{Name: "ci", Scopes: []string{model.ScopeRepoRead}})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	readRepo := func() int {
		req := httptest.NewRequest(http.MethodGet, "/api/repos/"+username+"/testrepo_"+suffix, nil)
		req.Header.Set("Authorization", "Bearer "+pat)
		return serve(h, req).Code
	}
	if code := readRepo(); code != http.StatusOK {
		t.Fatalf("PAT before reset: %d, want 200", code)
	}

	rr := serve(h, browserRequest(http.MethodPost, "/auth/password/forgot", "", url.Values{"email": {email}}))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Check your inbox") {
		t.Fatalf("forgot: %d, want the check-your-inbox page: %.300s", rr.Code, rr.Body.String())
	}
	token := box.NextTo(t, email).PasswordResetToken(t)

	rr = serve(h, browserRequest(http.MethodGet, resetPath(token), "", nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "@testpw_"+suffix) {
		t.Fatalf("GET reset: %d, want the form naming the account", rr.Code)
	}
	if rr.Header().Get("Referrer-Policy") != "no-referrer" || rr.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("GET reset headers = %v, want no-referrer and no-store", rr.Header())
	}

	rr = serve(h, browserRequest(http.MethodPost, resetPath(token), "", url.Values{"password": {resetNewPassword}, "confirm": {"something-else"}}))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "don&#39;t match") {
		t.Fatalf("mismatched POST: %d, want the form error: %.300s", rr.Code, rr.Body.String())
	}
	if link, _ := svc.PasswordReset.Check(context.Background(), token); link.State != model.PasswordResetPending {
		t.Fatalf("a mismatched confirmation spent the link: %s", link.State)
	}

	rr = serve(h, browserRequest(http.MethodPost, resetPath(token), "", url.Values{"password": {resetNewPassword}, "confirm": {resetNewPassword}}))
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/login?reset=done" {
		t.Fatalf("POST reset: %d to %q, want 303 to /login?reset=done", rr.Code, rr.Header().Get("Location"))
	}
	for _, c := range rr.Result().Cookies() {
		if c.Name == "cz_token" && c.Value != "" {
			t.Error("a reset signed the user in")
		}
	}
	if md := takeAuditMetadata(t, db, model.AuditActionPasswordReset, userID); md["issued_by"] != model.PasswordResetByEmail {
		t.Errorf("audit metadata = %v, want issued_by email", md)
	}
	if rr := serve(h, browserRequest(http.MethodGet, "/login?reset=done", "", nil)); !strings.Contains(rr.Body.String(), "Sign in with the new password") {
		t.Error("/login?reset=done shows no notice")
	}
	if rr := serve(h, browserRequest(http.MethodGet, "/settings", session, nil)); rr.Code == http.StatusOK {
		t.Error("the session from before the reset still opens /settings")
	}
	if code := readRepo(); code != http.StatusOK {
		t.Errorf("PAT after reset: %d, want 200", code)
	}
	if !isEmailVerified(t, db, userID) {
		t.Error("an emailed reset left the address unverified")
	}

	rr = serve(h, browserRequest(http.MethodGet, resetPath(token), "", nil))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("GET spent link: %d, want 400", rr.Code)
	}

	form := url.Values{"email": {email}, "password": {resetNewPassword}}
	login := serve(h, browserRequest(http.MethodPost, "/login", "", form))
	if login.Code != http.StatusSeeOther {
		t.Errorf("sign-in with the new password: %d, want 303", login.Code)
	}
}

func TestPasswordReset_AdminLinkSpends(t *testing.T) {
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	adminID := testutil.SeedSuperadmin(t, db, testutil.UniqueSuffix(t))
	userID, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")
	var username string
	if err := db.QueryRow(`SELECT username FROM users WHERE id = $1`, userID).Scan(&username); err != nil {
		t.Fatal(err)
	}
	_, link, err := svc.AdminUser.IssuePasswordResetLink(context.Background(), adminID, username)
	if err != nil {
		t.Fatal(err)
	}
	token := link[strings.LastIndex(link, "/")+1:]

	rr := serve(h, browserRequest(http.MethodPost, resetPath(token), "", url.Values{"password": {resetNewPassword}, "confirm": {resetNewPassword}}))
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/login?reset=done" {
		t.Fatalf("POST reset: %d to %q, want 303 to /login?reset=done", rr.Code, rr.Header().Get("Location"))
	}
	for _, c := range rr.Result().Cookies() {
		if c.Name == "cz_token" && c.Value != "" {
			t.Error("a reset signed the user in")
		}
	}
	if md := takeAuditMetadata(t, db, model.AuditActionPasswordReset, userID); md["issued_by"] != model.PasswordResetByAdmin {
		t.Errorf("audit metadata = %v, want issued_by admin", md)
	}
	if isEmailVerified(t, db, userID) {
		t.Error("an admin-issued link verified the address")
	}
}

func TestPasswordReset_ExpiredLinkIsGone(t *testing.T) {
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	userID, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")
	link, err := svc.PasswordReset.IssueLink(context.Background(), userID, model.PasswordResetByAdmin)
	if err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, db, `UPDATE password_reset_tokens SET expires_at = NOW() - INTERVAL '1 second' WHERE user_id = $1`, userID)
	path := strings.TrimPrefix(link, "http://localhost")
	if rr := serve(h, browserRequest(http.MethodGet, path, "", nil)); rr.Code != http.StatusGone {
		t.Errorf("GET expired: %d, want 410", rr.Code)
	}
	form := url.Values{"password": {resetNewPassword}, "confirm": {resetNewPassword}}
	if rr := serve(h, browserRequest(http.MethodPost, path, "", form)); rr.Code != http.StatusGone {
		t.Errorf("POST expired: %d, want 410", rr.Code)
	}
}

func TestPasswordReset_ForgotAnswersEveryAddressAlike(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	h, _, db := newVerificationRouter(t, smtp)
	suffix := testutil.UniqueSuffix(t)
	_, registered := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	testutil.SeedPasswordlessUser(t, db, suffix, "g-"+suffix)

	var want string
	for _, email := range []string{registered, "testnopw_" + suffix + "@test.invalid", "nobody_" + suffix + "@test.invalid"} {
		rr := serve(h, browserRequest(http.MethodPost, "/auth/password/forgot", "", url.Values{"email": {email}}))
		if rr.Code != http.StatusOK {
			t.Fatalf("forgot %s: %d", email, rr.Code)
		}
		if want == "" {
			want = rr.Body.String()
		} else if rr.Body.String() != want {
			t.Errorf("forgot %s answered differently from %s", email, registered)
		}
	}
	box.Drain(500 * time.Millisecond)

	rr := serve(h, browserRequest(http.MethodPost, "/auth/password/forgot", "", url.Values{"email": {"not an address"}}))
	if !strings.Contains(rr.Body.String(), "Enter a valid email address") {
		t.Errorf("malformed address: want the form error, got %.300s", rr.Body.String())
	}
}

func TestPasswordReset_RoutesAreRateLimited(t *testing.T) {
	h, _, _ := newVerificationRouter(t, config.SMTPConfig{})
	// The second route must start with a full budget, so neither shares the other's.
	for _, path := range []string{"/auth/password/forgot", resetPath("unknown")} {
		for i := 1; i <= 11; i++ {
			code := serve(h, browserRequest(http.MethodPost, path, "", url.Values{"email": {"x@test.invalid"}})).Code
			if limited := code == http.StatusTooManyRequests; limited != (i == 11) {
				t.Fatalf("POST %s: request %d got %d", path, i, code)
			}
		}
	}
}
