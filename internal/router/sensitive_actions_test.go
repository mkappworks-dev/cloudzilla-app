package router_test

// Integration tests for confirmed actions and ending sessions, through the real
// route table. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"io"
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
	gossh "golang.org/x/crypto/ssh"
)

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func sshPublicKey(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(gossh.MarshalAuthorizedKey(key)))
}

func withPassword(form url.Values, password string) url.Values {
	out := url.Values{}
	for k, v := range form {
		out[k] = v
	}
	if password != "" {
		out.Set("password", password)
	}
	return out
}

func htmxRequest(req *http.Request) *http.Request {
	req.Header.Set("HX-Request", "true")
	return req
}

// takeAudit waits for a goroutine-written audit row and removes it.
func takeAudit(t *testing.T, db *sql.DB, action string, actorID int64) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if countRows(t, db, `SELECT COUNT(*) FROM audit_log WHERE action = $1 AND actor_id = $2`, action, actorID) > 0 {
			testutil.Exec(t, db, `DELETE FROM audit_log WHERE action = $1 AND actor_id = $2`, action, actorID)
			return
		}
	}
	t.Errorf("no %s audit row for user %d", action, actorID)
}

// Each of these adds a way into the account that outlives the session, so a
// session alone must not be enough.
func TestSensitiveActions_NeedTheAccountsPassword(t *testing.T) {
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	ctx := context.Background()
	owner := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	app, _, err := svc.OAuthApp.CreateApp(ctx, owner, "Consent app", "", "", []string{testRedirectURI})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	// Each case gets its own user, since failed confirmations count per user.
	type user struct {
		id      int64
		email   string
		session string
	}
	newUser := func(t *testing.T) user {
		suffix := testutil.UniqueSuffix(t)
		id, email := testutil.SeedUserWithPassword(t, db, suffix, "password1")
		return user{id, email, makeJWT(t, id, "testpw_"+suffix)}
	}
	post := func(u user, path string, form url.Values) *http.Response {
		return serve(h, browserRequest(http.MethodPost, path, u.session, form)).Result()
	}

	tests := []struct {
		name    string
		path    string
		form    url.Values
		count   string
		refused func(*http.Response) bool
	}{
		{"personal access token", "/api/user/tokens", url.Values{"name": {"ci"}},
			`SELECT COUNT(*) FROM access_tokens WHERE user_id = $1`,
			func(r *http.Response) bool {
				return r.StatusCode == http.StatusSeeOther && strings.Contains(r.Header.Get("Location"), "profile_error=reauth_failed")
			}},
		{"SSH key", "/api/user/keys", url.Values{"title": {"laptop"}, "public_key": {sshPublicKey(t)}},
			`SELECT COUNT(*) FROM ssh_keys WHERE user_id = $1`,
			func(r *http.Response) bool { return r.StatusCode == http.StatusForbidden }},
		{"OAuth app approval", "/oauth/authorize", url.Values{"client_id": {app.ClientID}, "redirect_uri": {testRedirectURI}, "scope": {model.ScopeRepoRead}, "action": {"approve"}},
			`SELECT COUNT(*) FROM oauth_authorizations WHERE user_id = $1`,
			func(r *http.Response) bool { return r.StatusCode == http.StatusForbidden }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := newUser(t)
			before := countRows(t, db, tt.count, u.id)
			for _, password := range []string{"", "wrong"} {
				if resp := post(u, tt.path, withPassword(tt.form, password)); !tt.refused(resp) {
					t.Errorf("password %q: got %d to %q, want refused", password, resp.StatusCode, resp.Header.Get("Location"))
				}
			}
			if got := countRows(t, db, tt.count, u.id); got != before {
				t.Fatalf("an unconfirmed request added %d rows", got-before)
			}
			if resp := post(u, tt.path, withPassword(tt.form, "password1")); tt.refused(resp) || resp.StatusCode >= 400 {
				t.Fatalf("confirmed request: got %d to %q", resp.StatusCode, resp.Header.Get("Location"))
			}
			if got := countRows(t, db, tt.count, u.id); got != before+1 {
				t.Errorf("confirmed request added %d rows, want 1", got-before)
			}
		})
	}

	t.Run("deny needs no password", func(t *testing.T) {
		resp := post(newUser(t), "/oauth/authorize", url.Values{"client_id": {app.ClientID}, "redirect_uri": {testRedirectURI}, "scope": {model.ScopeRepoRead}, "action": {"deny"}})
		if resp.StatusCode != http.StatusSeeOther || !strings.Contains(resp.Header.Get("Location"), "error=access_denied") {
			t.Errorf("deny: got %d to %q, want the access_denied redirect", resp.StatusCode, resp.Header.Get("Location"))
		}
	})

	t.Run("email change", func(t *testing.T) {
		u := newUser(t)
		form := url.Values{"name": {"x"}, "email": {"sudo_" + testutil.UniqueSuffix(t) + "@test.invalid"}}
		if resp := post(u, "/settings/profile", form); !strings.Contains(resp.Header.Get("Location"), "profile_error=reauth_failed") {
			t.Fatalf("unconfirmed email change: redirected to %q", resp.Header.Get("Location"))
		}
		if got, _ := svc.User.GetByID(ctx, u.id); got.Email != u.email {
			t.Fatalf("unconfirmed change set the email to %q", got.Email)
		}
		if resp := post(u, "/settings/profile", withPassword(form, "password1")); !strings.Contains(resp.Header.Get("Location"), "profile_saved=1") {
			t.Fatalf("confirmed email change: redirected to %q", resp.Header.Get("Location"))
		}
		takeAudit(t, db, model.AuditActionEmailChange, u.id)
	})
}

func TestSensitiveActions_ShareOneFailureLimit(t *testing.T) {
	h, _, db := newVerificationRouter(t, config.SMTPConfig{})
	suffix := testutil.UniqueSuffix(t)
	userID, _ := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	session := makeJWT(t, userID, "testpw_"+suffix)

	for range 5 {
		serve(h, browserRequest(http.MethodPost, "/api/user/tokens", session, url.Values{"name": {"ci"}, "password": {"guess"}}))
	}
	rr := serve(h, browserRequest(http.MethodPost, "/api/user/keys", session, url.Values{
		"title": {"laptop"}, "public_key": {sshPublicKey(t)}, "password": {"password1"},
	}))
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("right password after 5 wrong ones elsewhere: got %d, want 429", rr.Code)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM ssh_keys WHERE user_id = $1`, userID); n != 0 {
		t.Errorf("%d keys added while throttled", n)
	}
}

func TestRevokeSessions_EndsEveryEarlierSession(t *testing.T) {
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	username, email := "testuser_"+suffix, "testuser_"+suffix+"@test.invalid"
	thisBrowser, err := svc.User.GenerateTokenForUser(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	otherBrowser := makeJWT(t, userID, username)
	settings := func(session string) int {
		return serve(h, browserRequest(http.MethodGet, "/settings", session, nil)).Code
	}
	if settings(thisBrowser) != http.StatusOK || settings(otherBrowser) != http.StatusOK {
		t.Fatal("precondition: both sessions should work")
	}

	rr := serve(h, browserRequest(http.MethodPost, "/settings/sessions/revoke", thisBrowser, url.Values{}))
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/settings?sessions_revoked=1#sessions" {
		t.Fatalf("revoke: got %d to %q", rr.Code, rr.Header().Get("Location"))
	}
	takeAudit(t, db, model.AuditActionSessionsRevoke, userID)
	fresh := responseCookie(t, rr, "cz_token").Value

	for name, session := range map[string]string{"the revoking browser's old session": thisBrowser, "another browser": otherBrowser} {
		if code := settings(session); code == http.StatusOK {
			t.Errorf("%s still reaches /settings", name)
		}
		// Optional-auth pages must treat it as signed out, not as the user.
		if page := serve(h, browserRequest(http.MethodGet, "/"+username, session, nil)); strings.Contains(page.Body.String(), email) {
			t.Errorf("%s still sees the owner's view of the profile", name)
		}
	}
	if code := settings(fresh); code != http.StatusOK {
		t.Errorf("the new session got %d on /settings, want 200", code)
	}
}

func TestSessions_EndWhenTheUserIsDeleted(t *testing.T) {
	h, _, db := newVerificationRouter(t, config.SMTPConfig{})
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	session := makeJWT(t, userID, "testuser_"+suffix)
	testutil.DeleteUsers(t, db, userID)

	rr := serve(h, browserRequest(http.MethodGet, "/settings", session, nil))
	if rr.Code != http.StatusSeeOther || !strings.HasPrefix(rr.Header().Get("Location"), "/login") {
		t.Errorf("deleted user's session: got %d to %q, want a redirect to /login", rr.Code, rr.Header().Get("Location"))
	}
}

// Each of these gives someone else lasting access to a repository or an
// organization, so a session alone must not be enough.
func TestAccessGrants_NeedTheAccountsPassword(t *testing.T) {
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	ctx := context.Background()
	granteeSuffix := testutil.UniqueSuffix(t)
	granteeID := testutil.SeedUser(t, db, granteeSuffix)
	grantee := "testuser_" + granteeSuffix

	type account struct {
		id            int64
		name, session string
	}
	newAccount := func(t *testing.T) account {
		suffix := testutil.UniqueSuffix(t)
		id, _ := testutil.SeedUserWithPassword(t, db, suffix, "password1")
		return account{id, "testpw_" + suffix, makeJWT(t, id, "testpw_"+suffix)}
	}
	newRepo := func(t *testing.T, a account) (int64, string) {
		repo, err := svc.Repo.Create(ctx, a.id, a.name, "grant_"+testutil.UniqueSuffix(t), "", true, service.RepoInitOptions{})
		if err != nil {
			t.Fatalf("create repo: %v", err)
		}
		return repo.ID, "/api/repos/" + a.name + "/" + repo.Name
	}
	newOrg := func(t *testing.T, a account) (int64, string) {
		org, err := svc.Org.Create(ctx, a.id, "grantorg_"+testutil.UniqueSuffix(t), "", "")
		if err != nil {
			t.Fatalf("create org: %v", err)
		}
		t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM organizations WHERE id = $1`, org.ID) })
		return org.ID, org.Name
	}
	isOrgOwner := func(orgID int64) func() int {
		return func() int {
			return countRows(t, db, `SELECT COUNT(*) FROM org_members WHERE org_id = $1 AND user_id = $2 AND role = 'owner'`, orgID, granteeID)
		}
	}
	form := func(a account, path string, values url.Values) func(string) *httptest.ResponseRecorder {
		return func(password string) *httptest.ResponseRecorder {
			return serve(h, browserRequest(http.MethodPost, path, a.session, withPassword(values, password)))
		}
	}
	dialog := func(a account, path string, values url.Values) func(string) *httptest.ResponseRecorder {
		return func(password string) *httptest.ResponseRecorder {
			return serve(h, htmxRequest(browserRequest(http.MethodPost, path, a.session, withPassword(values, password))))
		}
	}

	tests := []struct {
		name string
		// setup returns a request sent with the given password, and a count of what it grants.
		setup func(t *testing.T, a account) (func(string) *httptest.ResponseRecorder, func() int)
	}{
		{"collaborator", func(t *testing.T, a account) (func(string) *httptest.ResponseRecorder, func() int) {
			repoID, base := newRepo(t, a)
			return form(a, base+"/collaborators", url.Values{"username": {grantee}, "role": {"writer"}}),
				func() int {
					return countRows(t, db, `SELECT COUNT(*) FROM permissions WHERE repo_id = $1 AND user_id = $2`, repoID, granteeID)
				}
		}},
		{"deploy key", func(t *testing.T, a account) (func(string) *httptest.ResponseRecorder, func() int) {
			repoID, base := newRepo(t, a)
			return dialog(a, base+"/keys", url.Values{"title": {"ci"}, "public_key": {sshPublicKey(t)}}),
				func() int { return countRows(t, db, `SELECT COUNT(*) FROM deploy_keys WHERE repo_id = $1`, repoID) }
		}},
		{"webhook from the dialog", func(t *testing.T, a account) (func(string) *httptest.ResponseRecorder, func() int) {
			repoID, base := newRepo(t, a)
			return dialog(a, base+"/hooks", url.Values{"url": {"https://93.184.216.34/hook"}}),
				func() int { return countRows(t, db, `SELECT COUNT(*) FROM webhooks WHERE repo_id = $1`, repoID) }
		}},
		{"webhook from the API", func(t *testing.T, a account) (func(string) *httptest.ResponseRecorder, func() int) {
			repoID, base := newRepo(t, a)
			return func(password string) *httptest.ResponseRecorder {
					req := browserRequest(http.MethodPost, base+"/hooks", a.session, nil)
					req.Body = httpBody(`{"url":"https://93.184.216.34/hook","password":` + jsonString(password) + `}`)
					req.Header.Set("Content-Type", "application/json")
					return serve(h, req)
				},
				func() int { return countRows(t, db, `SELECT COUNT(*) FROM webhooks WHERE repo_id = $1`, repoID) }
		}},
		{"org owner", func(t *testing.T, a account) (func(string) *httptest.ResponseRecorder, func() int) {
			orgID, org := newOrg(t, a)
			return dialog(a, "/api/orgs/"+org+"/members", url.Values{"username": {grantee}, "role": {"owner"}}), isOrgOwner(orgID)
		}},
		{"promotion to owner", func(t *testing.T, a account) (func(string) *httptest.ResponseRecorder, func() int) {
			orgID, org := newOrg(t, a)
			testutil.Exec(t, db, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'member')`, orgID, granteeID)
			return dialog(a, "/api/orgs/"+org+"/members/"+grantee+"/role", url.Values{"role": {"owner"}}), isOrgOwner(orgID)
		}},
		{"org transfer", func(t *testing.T, a account) (func(string) *httptest.ResponseRecorder, func() int) {
			orgID, org := newOrg(t, a)
			return dialog(a, "/api/orgs/"+org+"/transfer", url.Values{"new_owner": {grantee}, "confirm_name": {org}}), isOrgOwner(orgID)
		}},
		{"making a repo public", func(t *testing.T, a account) (func(string) *httptest.ResponseRecorder, func() int) {
			repoID, base := newRepo(t, a)
			return form(a, strings.TrimPrefix(base, "/api/repos")+"/settings/visibility", url.Values{"private": {"false"}}),
				func() int {
					return countRows(t, db, `SELECT COUNT(*) FROM repositories WHERE id = $1 AND NOT private`, repoID)
				}
		}},
		{"deleting a repo", func(t *testing.T, a account) (func(string) *httptest.ResponseRecorder, func() int) {
			repoID, base := newRepo(t, a)
			return form(a, base+"/delete", url.Values{}),
				func() int {
					return countRows(t, db, `SELECT COUNT(*) FROM repositories WHERE id = $1 AND deleted_at IS NOT NULL`, repoID)
				}
		}},
		{"deleting an org", func(t *testing.T, a account) (func(string) *httptest.ResponseRecorder, func() int) {
			orgID, org := newOrg(t, a)
			return dialog(a, "/api/orgs/"+org+"/delete", url.Values{"confirm_name": {org}}),
				func() int { return countRows(t, db, `SELECT (1 - COUNT(*))::int FROM organizations WHERE id = $1`, orgID) }
		}},
		{"repo transfer", func(t *testing.T, a account) (func(string) *httptest.ResponseRecorder, func() int) {
			repoID, base := newRepo(t, a)
			return form(a, base+"/transfer", url.Values{"new_owner": {grantee}}),
				func() int {
					return countRows(t, db, `SELECT COUNT(*) FROM repositories WHERE id = $1 AND owner_id = $2`, repoID, granteeID)
				}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			send, granted := tt.setup(t, newAccount(t))
			for _, password := range []string{"", "wrong"} {
				rr := send(password)
				if !strings.Contains(rr.Body.String(), "incorrect") || (rr.Code < 400 && rr.Header().Get("HX-Retarget") == "") {
					t.Errorf("password %q: got %d %s, want the refusal", password, rr.Code, rr.Body)
				}
			}
			if n := granted(); n != 0 {
				t.Fatalf("an unconfirmed request granted access (%d)", n)
			}
			if rr := send("password1"); rr.Code >= 400 || rr.Header().Get("HX-Retarget") != "" {
				t.Fatalf("confirmed request: got %d %s", rr.Code, rr.Body)
			}
			if n := granted(); n != 1 {
				t.Errorf("the confirmed request granted %d, want 1", n)
			}
		})
	}

	// Members get no repository until added to one, so these stay one click.
	t.Run("member and demotion need no password", func(t *testing.T) {
		a := newAccount(t)
		orgID, org := newOrg(t, a)
		if rr := dialog(a, "/api/orgs/"+org+"/members", url.Values{"username": {grantee}, "role": {"member"}})(""); rr.Code != http.StatusOK || rr.Header().Get("HX-Retarget") != "" {
			t.Fatalf("add member: got %d %s", rr.Code, rr.Body)
		}
		testutil.Exec(t, db, `UPDATE org_members SET role = 'owner' WHERE org_id = $1 AND user_id = $2`, orgID, granteeID)
		if rr := dialog(a, "/api/orgs/"+org+"/members/"+grantee+"/role", url.Values{"role": {"member"}})(""); rr.Code != http.StatusOK {
			t.Fatalf("demote: got %d %s", rr.Code, rr.Body)
		}
		if isOrgOwner(orgID)() != 0 {
			t.Error("the demotion didn't take")
		}
	})

	// A non-owner's confirmation would spend their attempts on a request that fails anyway.
	t.Run("a non-owner is refused before confirming", func(t *testing.T) {
		owner, member := newAccount(t), newAccount(t)
		orgID, org := newOrg(t, owner)
		testutil.Exec(t, db, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'member')`, orgID, member.id)
		for range 5 {
			if rr := form(member, "/api/orgs/"+org+"/members", url.Values{"username": {grantee}, "role": {"owner"}})("wrong"); rr.Code != http.StatusForbidden || strings.Contains(rr.Body.String(), "incorrect") {
				t.Fatalf("non-owner: got %d %s, want 403 without a confirmation", rr.Code, rr.Body)
			}
		}
		if _, err := svc.Reauth.Confirm(ctx, member.id, service.Confirmation{Password: "password1"}); err != nil {
			t.Errorf("the refused requests used up the member's attempts: %v", err)
		}
	})
}

func TestTOTP_TurningItOnOrOffNeedsThePassword(t *testing.T) {
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID, _ := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	session := makeJWT(t, userID, "testpw_"+suffix)
	enabled := func() bool {
		on, _, err := svc.TOTP.GetUserTOTPState(ctx, userID)
		if err != nil {
			t.Fatal(err)
		}
		return on
	}
	post := func(path string, form url.Values) string {
		rr := serve(h, browserRequest(http.MethodPost, path, session, form))
		if rr.Code != http.StatusSeeOther {
			t.Fatalf("%s: got %d %s", path, rr.Code, rr.Body)
		}
		return rr.Header().Get("Location")
	}
	if err := svc.TOTP.StoreSecret(ctx, userID, testutil.TestTOTPSecret); err != nil {
		t.Fatal(err)
	}
	enable := url.Values{"secret": {testutil.TestTOTPSecret}, "code": {testutil.TOTPCode(t, testutil.TestTOTPSecret)}}

	for _, password := range []string{"", "wrong"} {
		if loc := post("/api/user/totp/enable", withPassword(enable, password)); !strings.Contains(loc, "profile_error=reauth_failed") {
			t.Errorf("enable with password %q: redirected to %q", password, loc)
		}
	}
	if enabled() {
		t.Fatal("an unconfirmed request turned 2FA on")
	}
	if loc := post("/api/user/totp/enable", withPassword(enable, "password1")); loc != "/settings#security" || !enabled() {
		t.Fatalf("confirmed enable: redirected to %q, enabled = %v", loc, enabled())
	}

	disable := url.Values{"code": {testutil.TOTPCode(t, testutil.TestTOTPSecret)}}
	if loc := post("/api/user/totp/disable", disable); !strings.Contains(loc, "profile_error=reauth_failed") || !enabled() {
		t.Fatalf("disable without the password: redirected to %q, enabled = %v", loc, enabled())
	}
	if loc := post("/api/user/totp/disable", withPassword(disable, "password1")); loc != "/settings#security" || enabled() {
		t.Errorf("confirmed disable: redirected to %q, enabled = %v", loc, enabled())
	}
}

// The code page comes after the password, so without a limit a stolen password
// could guess its way past 2FA.
func TestTwoFactorSignIn_IsThrottled(t *testing.T) {
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	userID, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")
	testutil.EnableTOTP(t, db, userID)
	pending, err := svc.TOTP.GeneratePendingToken(userID, testJWTSecret, nil)
	if err != nil {
		t.Fatal(err)
	}
	verify := func(code string) *httptest.ResponseRecorder {
		return postForm(h, "/auth/2fa/verify", url.Values{"code": {code}}, &http.Cookie{Name: "cz_totp_pending", Value: pending})
	}

	for i := range 5 {
		if rr := verify("000000"); !strings.Contains(rr.Body.String(), "Invalid code") {
			t.Fatalf("wrong code %d: got %d, want the invalid-code page", i, rr.Code)
		}
	}
	rr := verify(testutil.TOTPCode(t, testutil.TestTOTPSecret))
	if !strings.Contains(rr.Body.String(), "Too many incorrect codes") {
		t.Errorf("right code after 5 wrong ones: got %d, want the throttle message", rr.Code)
	}
	for _, c := range rr.Result().Cookies() {
		if c.Name == "cz_token" && c.Value != "" {
			t.Error("a throttled sign-in started a session")
		}
	}
}

// Someone else who knows the password is signed out by the change.
func TestChangePassword_NeedsTheCurrentPasswordAndEndsEverySession(t *testing.T) {
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID, email := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	thisBrowser, err := svc.User.GenerateTokenForUser(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	otherBrowser := makeJWT(t, userID, "testpw_"+suffix)
	settings := func(session string) int {
		return serve(h, browserRequest(http.MethodGet, "/settings", session, nil)).Code
	}
	change := func(form url.Values) *httptest.ResponseRecorder {
		return serve(h, browserRequest(http.MethodPost, "/settings/password", thisBrowser, form))
	}
	newPassword := func(p, confirm string) url.Values {
		return url.Values{"new_password": {p}, "new_password_confirm": {confirm}}
	}

	for _, tt := range []struct {
		name string
		form url.Values
		code string
	}{
		{"no current password", newPassword("password2", "password2"), "reauth_failed"},
		{"wrong current password", withPassword(newPassword("password2", "password2"), "wrong"), "reauth_failed"},
		{"mismatch", withPassword(newPassword("password2", "password3"), "password1"), "password_mismatch"},
		{"too short", withPassword(newPassword("short", "short"), "password1"), "password_too_short"},
	} {
		rr := change(tt.form)
		if want := "/settings?password_error=" + tt.code + "#password"; rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != want {
			t.Errorf("%s: got %d to %q, want %q", tt.name, rr.Code, rr.Header().Get("Location"), want)
		}
	}
	if settings(otherBrowser) != http.StatusOK {
		t.Fatal("a refused change ended the other session")
	}

	rr := change(withPassword(newPassword("password2", "password2"), "password1"))
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/settings?password_changed=1#password" {
		t.Fatalf("change: got %d to %q", rr.Code, rr.Header().Get("Location"))
	}
	takeAudit(t, db, model.AuditActionPasswordChange, userID)
	fresh := responseCookie(t, rr, "cz_token").Value
	for name, session := range map[string]string{"this browser's old session": thisBrowser, "another browser": otherBrowser} {
		if code := settings(session); code == http.StatusOK {
			t.Errorf("%s still reaches /settings", name)
		}
	}
	if code := settings(fresh); code != http.StatusOK {
		t.Errorf("the new session got %d on /settings, want 200", code)
	}
	if _, _, err := svc.User.Authenticate(ctx, email, "password2"); err != nil {
		t.Errorf("the new password doesn't sign in: %v", err)
	}
}

func httpBody(s string) io.ReadCloser { return io.NopCloser(strings.NewReader(s)) }

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// A personal access token was created with the password, so a script using one
// administers repositories and orgs without a prompt. It still can't add a way
// into the account itself.
func TestPAT_SkipsConfirmationOnlyForRepoAndOrgAdministration(t *testing.T) {
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID, _ := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	owner := "testpw_" + suffix
	granteeSuffix := testutil.UniqueSuffix(t)
	testutil.SeedUser(t, db, granteeSuffix)
	pat, _, err := svc.AccessToken.Generate(ctx, userID, "ci", nil, nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	repo, err := svc.Repo.Create(ctx, userID, owner, "pat_"+suffix, "", true, service.RepoInitOptions{})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	base := "/api/repos/" + owner + "/" + repo.Name
	post := func(token, path string, form url.Values) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", "Bearer "+token)
		return serve(h, req)
	}

	if rr := post(pat, base+"/collaborators", url.Values{"username": {"testuser_" + granteeSuffix}, "role": {"reader"}}); rr.Code >= 400 {
		t.Errorf("collaborator with a PAT: got %d %s", rr.Code, rr.Body)
	}
	if rr := post(pat, base+"/keys", url.Values{"title": {"ci"}, "public_key": {sshPublicKey(t)}}); rr.Code >= 400 {
		t.Errorf("deploy key with a PAT: got %d %s", rr.Code, rr.Body)
	}
	if rr := post(pat, "/api/user/keys", url.Values{"title": {"laptop"}, "public_key": {sshPublicKey(t)}}); rr.Code != http.StatusForbidden {
		t.Errorf("account SSH key with a PAT and no password: got %d, want 403", rr.Code)
	}
	session := makeJWT(t, userID, owner)
	if rr := post(session, base+"/collaborators", url.Values{"username": {"testuser_" + granteeSuffix}, "role": {"writer"}}); rr.Code != http.StatusForbidden {
		t.Errorf("a session JWT sent as a bearer token skipped the confirmation: got %d", rr.Code)
	}
}

// An account with no password or 2FA confirms with a code mailed to it; with
// no outgoing email it has no way to, and is refused.
func TestEmailCode_ConfirmsForAccountsWithoutAPasswordOr2FA(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	h, _, db := newVerificationRouter(t, smtp)
	withoutSMTP, _, _ := newVerificationRouter(t, config.SMTPConfig{})
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedPasswordlessUser(t, db, suffix, "g_route_"+suffix)
	session := makeJWT(t, userID, "testnopw_"+suffix)
	addKey := func(h http.Handler, code string) *httptest.ResponseRecorder {
		return serve(h, browserRequest(http.MethodPost, "/api/user/keys", session, url.Values{
			"title": {"laptop"}, "public_key": {sshPublicKey(t)}, "email_code": {code},
		}))
	}

	if rr := addKey(withoutSMTP, ""); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "no password, two-factor app or email") {
		t.Errorf("without email: got %d %s, want the no-way-to-confirm refusal", rr.Code, rr.Body)
	}
	if rr := addKey(h, ""); rr.Code != http.StatusForbidden {
		t.Fatalf("no code: got %d, want 403", rr.Code)
	}
	rr := serve(h, htmxRequest(browserRequest(http.MethodPost, "/settings/confirm-code", session, url.Values{})))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Code sent") {
		t.Fatalf("send code: got %d %s", rr.Code, rr.Body)
	}
	code := box.NextTo(t, "testnopw_"+suffix+"@test.invalid").ConfirmationCode(t)
	if rr := addKey(h, code); rr.Code >= 400 {
		t.Fatalf("with the emailed code: got %d %s", rr.Code, rr.Body)
	}
	if rr := addKey(h, code); rr.Code != http.StatusForbidden {
		t.Errorf("the same code again: got %d, want 403", rr.Code)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM ssh_keys WHERE user_id = $1`, userID); n != 1 {
		t.Errorf("%d keys added, want 1", n)
	}

	// Forms read the code through confirmationFrom, not a JSON body.
	testutil.Exec(t, db, `UPDATE users SET reauth_code_sent_at = NULL WHERE id = $1`, userID)
	serve(h, htmxRequest(browserRequest(http.MethodPost, "/settings/confirm-code", session, url.Values{})))
	code = box.NextTo(t, "testnopw_"+suffix+"@test.invalid").ConfirmationCode(t)
	rr = serve(h, browserRequest(http.MethodPost, "/api/user/tokens", session, url.Values{"name": {"ci"}, "email_code": {code}}))
	if rr.Header().Get("Location") != "/settings#tokens" {
		t.Errorf("token with the emailed code: got %d to %q", rr.Code, rr.Header().Get("Location"))
	}
}

// A stolen superadmin session could otherwise open the instance up to its
// holder: invite them, vouch for an address, or point sign-in at their own IdP.
func TestAdminActions_NeedThePassword(t *testing.T) {
	h, _, db := newVerificationRouter(t, config.SMTPConfig{})
	suffix := testutil.UniqueSuffix(t)
	adminID := testutil.SeedSuperadmin(t, db, suffix)
	testutil.SetPassword(t, db, adminID, "password1")
	admin := superadminJWT(t, adminID, "testadmin_"+suffix)
	targetID := testutil.SeedUser(t, db, suffix+"_t")
	invitee := "invitee_" + suffix + "@test.invalid"
	post := func(path string, form url.Values) *httptest.ResponseRecorder {
		return serve(h, htmxRequest(browserRequest(http.MethodPost, path, admin, form)))
	}
	// Other tests share the instance's settings, so compare them rather than assume them.
	snapshot := func() string {
		var s string
		if err := db.QueryRowContext(context.Background(),
			`SELECT COALESCE((SELECT string_agg(provider || config::text || enabled::text, ',' ORDER BY provider) FROM sso_configs), '')
			     || COALESCE((SELECT value FROM site_settings WHERE key = 'allow_registration'), '')`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	settingsBefore := snapshot()
	var registration string
	if err := db.QueryRowContext(context.Background(), `SELECT value FROM site_settings WHERE key = 'allow_registration'`).Scan(&registration); err != nil {
		t.Fatal(err)
	}
	// A refusal that doesn't happen would leave the shared setting changed for other tests.
	t.Cleanup(func() {
		testutil.Exec(t, db, `UPDATE site_settings SET value = $1 WHERE key = 'allow_registration'`, registration)
	})
	unchanged := func() bool {
		return countRows(t, db, `SELECT COUNT(*) FROM invitations WHERE email = $1`, invitee) == 0 &&
			countRows(t, db, `SELECT COUNT(*) FROM users WHERE id = $1 AND email_verified_at IS NOT NULL`, targetID) == 0 &&
			snapshot() == settingsBefore
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM invitations WHERE email = $1`, invitee) })

	for _, tt := range []struct {
		name, path string
		form       url.Values
	}{
		{"site setting", "/api/admin/settings", url.Values{"key": {"allow_registration"}, "value": {"flipped"}}},
		{"invitation", "/api/admin/invitations", url.Values{"email": {invitee}}},
		{"manual verification", "/api/admin/users/verify-email", url.Values{"username": {"testuser_" + suffix + "_t"}, "email": {"testuser_" + suffix + "_t@test.invalid"}}},
		{"SSO", "/admin/sso", url.Values{"provider": {"ldap"}, "ldap_host": {"ldap.attacker-" + suffix + ".invalid"}, "ldap_port": {"389"}}},
	} {
		for _, password := range []string{"", "wrong"} {
			if rr := post(tt.path, withPassword(tt.form, password)); !strings.Contains(rr.Body.String(), "was incorrect") {
				t.Errorf("%s with password %q: got %d %.200s, want the refusal", tt.name, password, rr.Code, rr.Body)
			}
		}
		// Stay under the attempt limit, so each case is refused for its own confirmation.
		testutil.Exec(t, db, `UPDATE users SET reauth_failures = 0 WHERE id = $1`, adminID)
	}
	if !unchanged() {
		t.Fatal("an unconfirmed admin request changed something")
	}
	if rr := post("/api/admin/invitations", withPassword(url.Values{"email": {invitee}}, "password1")); rr.Code != http.StatusOK {
		t.Errorf("confirmed invitation: got %d %s", rr.Code, rr.Body)
	}
}

func TestDeleteAccount_NeedsThePassword(t *testing.T) {
	h, _, db := newVerificationRouter(t, config.SMTPConfig{})
	suffix := testutil.UniqueSuffix(t)
	userID, _ := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	session := makeJWT(t, userID, "testpw_"+suffix)
	remove := func(password string) *httptest.ResponseRecorder {
		return serve(h, browserRequest(http.MethodPost, "/settings/delete-account", session,
			withPassword(url.Values{"confirm_username": {"testpw_" + suffix}}, password)))
	}

	if rr := remove("wrong"); rr.Header().Get("Location") != "/settings?profile_error=reauth_failed#delete" {
		t.Fatalf("wrong password: redirected to %q", rr.Header().Get("Location"))
	}
	if countRows(t, db, `SELECT COUNT(*) FROM users WHERE id = $1`, userID) != 1 {
		t.Fatal("an unconfirmed request deleted the account")
	}
	if rr := remove("password1"); rr.Header().Get("Location") != "/" {
		t.Fatalf("confirmed: got %d to %q", rr.Code, rr.Header().Get("Location"))
	}
}
