package handler_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const (
	linkPassword   = "link password 1"
	linkCookieName = "oauth_link_state"
)

// linkEnv serves the connected-accounts routes with the middleware router.go gives
// them. CSRF and OAuth-app tokens are covered through the real router in internal/router.
type linkEnv struct {
	t      *testing.T
	db     *sql.DB
	router http.Handler
	google string
	// info is what the fake Google reports about the signed-in Google account.
	info map[string]any
}

type linkUser struct {
	id      int64
	name    string
	email   string
	session string
}

func newLinkEnv(t *testing.T) *linkEnv {
	return newLinkEnvWithClientID(t, "test-client")
}

func newLinkEnvWithClientID(t *testing.T, clientID string) *linkEnv {
	t.Helper()
	e := &linkEnv{t: t, db: testutil.OpenTestDB(t), info: map[string]any{}}
	e.google = fakeGoogleServing(t, &e.info)
	cfg := &config.Config{
		Auth:  config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: time.Hour, CookieName: testCookieName},
		OAuth: config.OAuthConfig{GoogleClientID: clientID, GoogleClientSecret: "test-secret", GoogleRedirectURL: "http://localhost/auth/google/callback"},
		Git:   config.GitConfig{ReposRoot: t.TempDir()},
	}
	svc := service.New(store.New(e.db), cfg)
	h := handler.New(svc, cfg)
	unauthorized := func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) }
	authMW := middleware.Auth(testJWTSecret, testCookieName, svc.AccessToken, svc.OAuthApp, unauthorized)
	optAuthMW := middleware.OptionalAuth(testJWTSecret, testCookieName, svc.AccessToken, svc.OAuthApp)
	r := chi.NewRouter()
	r.With(authMW).Get("/settings", h.PageSettings)
	r.With(authMW).Post("/settings/connected-accounts/google", h.ConnectGoogle)
	r.With(authMW).Post("/settings/connected-accounts/google/disconnect", h.DisconnectGoogle)
	r.With(optAuthMW).Get("/auth/google/callback", h.GoogleOAuthCallback)
	e.router = r
	return e
}

func (e *linkEnv) user() linkUser {
	e.t.Helper()
	suffix := testutil.UniqueSuffix(e.t)
	id, email := testutil.SeedUserWithPassword(e.t, e.db, suffix, linkPassword)
	e.clearAuditAtEnd(id)
	name := "testpw_" + suffix
	return linkUser{id: id, name: name, email: email, session: makeIssueJWT(e.t, id, name)}
}

func (e *linkEnv) passwordlessUser(googleID string) linkUser {
	e.t.Helper()
	suffix := testutil.UniqueSuffix(e.t)
	id := testutil.SeedPasswordlessUser(e.t, e.db, suffix, googleID)
	e.clearAuditAtEnd(id)
	name := "testnopw_" + suffix
	return linkUser{id: id, name: name, email: name + "@test.invalid", session: makeIssueJWT(e.t, id, name)}
}

// Registered after the user's own cleanup, so it runs before the user is deleted.
func (e *linkEnv) clearAuditAtEnd(userID int64) {
	e.t.Cleanup(func() { testutil.Exec(e.t, e.db, `DELETE FROM audit_log WHERE actor_id = $1`, userID) })
}

func (e *linkEnv) googleAccount(id, email string, verified bool) {
	e.info = map[string]any{"id": id, "email": email, "verified_email": verified, "name": "Linked Person"}
}

func (e *linkEnv) post(path, session string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: testCookieName, Value: session})
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

func (e *linkEnv) settingsBody(path, session string, cookies ...*http.Cookie) string {
	e.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(&http.Cookie{Name: testCookieName, Value: session})
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		e.t.Fatalf("GET %s: %d", path, rr.Code)
	}
	return rr.Body.String()
}

func (e *linkEnv) securitySection(session string, cookies ...*http.Cookie) string {
	e.t.Helper()
	return htmlSection(e.t, e.settingsBody("/settings", session, cookies...), "security")
}

func (e *linkEnv) connect(u linkUser, password, code string) *httptest.ResponseRecorder {
	return e.post("/settings/connected-accounts/google", u.session, url.Values{"password": {password}, "code": {code}})
}

func (e *linkEnv) disconnect(u linkUser, password, code string) *httptest.ResponseRecorder {
	return e.post("/settings/connected-accounts/google/disconnect", u.session, url.Values{"password": {password}, "code": {code}})
}

// startLink connects u with the right password and returns the state Google will echo back.
func (e *linkEnv) startLink(u linkUser) string {
	e.t.Helper()
	return e.startLinkWithCode(u, "")
}

func (e *linkEnv) startLinkWithCode(u linkUser, code string) string {
	e.t.Helper()
	rr := e.connect(u, linkPassword, code)
	loc, err := url.Parse(rr.Header().Get("Location"))
	if rr.Code != http.StatusSeeOther || err != nil || !strings.HasPrefix(loc.String(), e.google+"/auth") {
		e.t.Fatalf("connect: got %d to %q, want 303 to the fake Google", rr.Code, rr.Header().Get("Location"))
	}
	state := loc.Query().Get("state")
	c := responseCookie(rr, linkCookieName)
	if c == nil || c.Value != state || state == "" {
		e.t.Fatalf("connect set link cookie %+v for state %q", c, state)
	}
	if !c.HttpOnly || c.Path != "/auth/google/callback" || c.SameSite != http.SameSiteLaxMode {
		e.t.Errorf("link cookie HttpOnly=%v Path=%q SameSite=%v; want HttpOnly, the callback path, and Lax", c.HttpOnly, c.Path, c.SameSite)
	}
	if got := loc.Query().Get("prompt"); got != "select_account" {
		e.t.Errorf("auth URL prompt = %q, want select_account so the user picks the Google account", got)
	}
	return state
}

// callback is Google redirecting the browser back; session and linkState are its cookies ("" for none).
func (e *linkEnv) callback(session, linkState, state string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=c1&state="+url.QueryEscape(state), nil)
	if session != "" {
		req.AddCookie(&http.Cookie{Name: testCookieName, Value: session})
	}
	if linkState != "" {
		req.AddCookie(&http.Cookie{Name: linkCookieName, Value: linkState})
	}
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

func (e *linkEnv) linkedGoogleID(userID int64) string {
	e.t.Helper()
	var provider, id string
	if err := e.db.QueryRowContext(context.Background(), `SELECT oauth_provider, oauth_id FROM users WHERE id = $1`, userID).Scan(&provider, &id); err != nil {
		e.t.Fatalf("read oauth link: %v", err)
	}
	if provider != "google" {
		return ""
	}
	return id
}

// awaitAudit waits for the audit goroutine to write action for actorID.
func (e *linkEnv) awaitAudit(actorID int64, action string) bool {
	e.t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		var n int
		err := e.db.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM audit_log WHERE actor_id = $1 AND action = $2 AND target_id = $1`, actorID, action).Scan(&n)
		if err != nil {
			e.t.Fatalf("read audit log: %v", err)
		}
		if n > 0 {
			return true
		}
	}
	return false
}

// auditOAuthID returns the oauth_id an already-written audit entry records.
func (e *linkEnv) auditOAuthID(actorID int64, action string) string {
	e.t.Helper()
	var id string
	err := e.db.QueryRowContext(context.Background(),
		`SELECT metadata->>'oauth_id' FROM audit_log WHERE actor_id = $1 AND action = $2`, actorID, action).Scan(&id)
	if err != nil {
		e.t.Fatalf("read %s audit entry: %v", action, err)
	}
	return id
}

func responseCookie(rr *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rr.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// settingsError returns the profile_error a redirect to /settings carries, failing if it goes elsewhere.
func settingsError(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	loc, err := url.Parse(rr.Header().Get("Location"))
	if rr.Code != http.StatusSeeOther || err != nil || loc.Path != "/settings" {
		t.Fatalf("got %d to %q, want a 303 back to /settings; body: %.300s", rr.Code, rr.Header().Get("Location"), rr.Body.String())
	}
	return loc.Query().Get("profile_error")
}

func assertNoSignIn(t *testing.T, rr *httptest.ResponseRecorder) {
	t.Helper()
	if hasAuthCookie(rr) {
		t.Error("the link callback issued a session cookie")
	}
}

const (
	connectAction    = `action="/settings/connected-accounts/google"`
	disconnectAction = `action="/settings/connected-accounts/google/disconnect"`
)

func TestPageSettings_OffersConnectWithReauthentication(t *testing.T) {
	e := newLinkEnv(t)
	u := e.user()

	section := e.securitySection(u.session)
	assertContains(t, section, `id="connected-accounts"`)
	assertContains(t, section, "Not connected")
	assertContains(t, section, connectAction)
	assertContains(t, section, `name="password"`)
	if strings.Contains(section, `id="google-connect-code"`) {
		t.Error("the connect form asks for a two-factor code on an account without 2FA")
	}

	testutil.EnableTOTP(t, e.db, u.id)
	assertContains(t, e.securitySection(u.session), `id="google-connect-code"`)
}

func TestPageSettings_HidesConnectWhenGoogleIsNotConfigured(t *testing.T) {
	e := newLinkEnvWithClientID(t, "")
	u := e.user()

	section := e.securitySection(u.session)
	if strings.Contains(section, connectAction) {
		t.Error("a Connect Google form is shown although Google sign-in is not configured")
	}
	assertContains(t, section, "set up on this instance")
}

func TestPageSettings_ConnectedAccountOffersDisconnect(t *testing.T) {
	e := newLinkEnv(t)
	u := e.user()
	googleID := "g_page_" + testutil.UniqueSuffix(t)
	e.googleAccount(googleID, googleID+"@gmail.test", true)
	state := e.startLink(u)
	rr := e.callback(u.session, state, state)
	notice := responseCookie(rr, "cz_settings_notice")
	if notice == nil {
		t.Fatal("the link callback set no settings notice")
	}

	section := e.securitySection(u.session, notice)
	assertContains(t, section, "Google account connected")
	assertContains(t, section, ">Connected<")
	assertContains(t, section, disconnectAction)
	if strings.Contains(section, connectAction) {
		t.Error("a connected account is still offered Connect Google")
	}
	if again := e.securitySection(u.session); strings.Contains(again, "Google account connected") {
		t.Error("the notice shows again without its cookie")
	}
	e.awaitAudit(u.id, model.AuditActionOAuthConnect)
}

func TestPageSettings_ShowsEachErrorBesideItsControl(t *testing.T) {
	e := newLinkEnv(t)
	u := e.user()

	google := e.settingsBody("/settings?profile_error=google_link_taken", u.session)
	if i := strings.Index(google, `id="connected-accounts"`); i < 0 || !strings.Contains(google[i:], "connected to a different Cloudzilla account") {
		t.Error("a Google connect error is not shown under Connected accounts")
	}
	totp := e.settingsBody("/settings?profile_error=totp_invalid_code", u.session)
	assertContains(t, totp, "Invalid verification code")
	if strings.Contains(htmlSection(t, totp, "security"), `role="alert"`) {
		t.Error("a two-factor error was shown under Connected accounts")
	}
}

func TestPageSettings_PasswordlessAccountCannotDisconnect(t *testing.T) {
	e := newLinkEnv(t)
	u := e.passwordlessUser("g_pageonly_" + testutil.UniqueSuffix(t))

	section := e.securitySection(u.session)
	if strings.Contains(section, disconnectAction) {
		t.Error("an account without a password is offered a working Disconnect")
	}
	assertContains(t, section, "lock you out")
}

func TestConnectGoogle_WrongPasswordIsRefused(t *testing.T) {
	e := newLinkEnv(t)
	u := e.user()

	rr := e.connect(u, "not the password", "")

	if got := settingsError(t, rr); got != "google_reauth_failed" {
		t.Errorf("profile_error = %q, want google_reauth_failed", got)
	}
	if c := responseCookie(rr, linkCookieName); c != nil && c.Value != "" {
		t.Error("a refused connect still set a link state cookie")
	}
}

func TestConnectGoogle_TOTPAccountNeedsTheCode(t *testing.T) {
	e := newLinkEnv(t)
	u := e.user()
	testutil.EnableTOTP(t, e.db, u.id)
	googleID := "g_totp_" + testutil.UniqueSuffix(t)
	e.googleAccount(googleID, googleID+"@gmail.test", true)

	for _, code := range []string{"", "12345"} {
		if got := settingsError(t, e.connect(u, linkPassword, code)); got != "google_reauth_failed" {
			t.Errorf("TOTP code %q: profile_error = %q, want google_reauth_failed", code, got)
		}
	}
	state := e.startLinkWithCode(u, testutil.TOTPCode(t, testutil.TestTOTPSecret))
	if got := settingsError(t, e.callback(u.session, state, state)); got != "" {
		t.Fatalf("callback after the right code: profile_error = %q", got)
	}
	if got := e.linkedGoogleID(u.id); got != googleID {
		t.Errorf("linked Google ID = %q, want %q", got, googleID)
	}
	e.awaitAudit(u.id, model.AuditActionOAuthConnect)
}

func TestConnectGoogle_RefusedWhenGoogleIsNotConfigured(t *testing.T) {
	e := newLinkEnvWithClientID(t, "")
	u := e.user()

	if got := settingsError(t, e.connect(u, linkPassword, "")); got != "google_not_configured" {
		t.Errorf("profile_error = %q, want google_not_configured", got)
	}
}

func TestConnectGoogle_LinksWithoutSigningInAndGoogleSignInThenWorks(t *testing.T) {
	e := newLinkEnv(t)
	u := e.user()
	googleID := "g_connect_" + testutil.UniqueSuffix(t)
	googleEmail := googleID + "@gmail.test"
	e.googleAccount(googleID, googleEmail, true)

	state := e.startLink(u)
	rr := e.callback(u.session, state, state)

	if got := settingsError(t, rr); got != "" {
		t.Fatalf("profile_error = %q, want a clean return to settings", got)
	}
	assertNoSignIn(t, rr)
	if c := responseCookie(rr, linkCookieName); c == nil || c.MaxAge >= 0 {
		t.Error("the link state cookie was not cleared")
	}
	if got := e.linkedGoogleID(u.id); got != googleID {
		t.Fatalf("linked Google ID = %q, want %q", got, googleID)
	}
	if !e.awaitAudit(u.id, model.AuditActionOAuthConnect) {
		t.Error("no user.oauth.connect audit entry")
	} else if got := e.auditOAuthID(u.id, model.AuditActionOAuthConnect); got != googleID {
		t.Errorf("connect audit entry records oauth_id %q, want %q", got, googleID)
	}
	var created int
	if err := e.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM users WHERE email = $1`, googleEmail).Scan(&created); err != nil || created != 0 {
		t.Errorf("link mode created %d accounts for the Google address (err %v)", created, err)
	}

	login := e.callbackLoginMode()
	if !hasAuthCookie(login) {
		t.Fatalf("Google sign-in after connecting: got %d without a session; body: %.300s", login.Code, login.Body.String())
	}
	if sub := sessionSubject(t, responseCookie(login, testCookieName).Value); sub != u.id {
		t.Errorf("Google sign-in opened account %d, want the connected account %d", sub, u.id)
	}
	e.awaitAudit(u.id, model.AuditActionLogin)
}

func (e *linkEnv) callbackLoginMode() *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?state=s1&code=c1", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "s1"})
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

func sessionSubject(t *testing.T, token string) int64 {
	t.Helper()
	parsed, err := jwt.Parse(token, func(*jwt.Token) (any, error) { return []byte(testJWTSecret), nil })
	if err != nil {
		t.Fatalf("parse session: %v", err)
	}
	sub, _ := parsed.Claims.(jwt.MapClaims)["sub"].(float64)
	return int64(sub)
}

func TestGoogleLinkCallback_ReplayedStateIsRefused(t *testing.T) {
	e := newLinkEnv(t)
	u := e.user()
	googleID := "g_replay_" + testutil.UniqueSuffix(t)
	e.googleAccount(googleID, googleID+"@gmail.test", false)

	state := e.startLink(u)
	if got := settingsError(t, e.callback(u.session, state, state)); got != "google_link_unverified" {
		t.Fatalf("first callback: profile_error = %q, want google_link_unverified", got)
	}
	e.googleAccount(googleID, googleID+"@gmail.test", true)
	rr := e.callback(u.session, state, state)

	if got := settingsError(t, rr); got != "google_link_invalid" {
		t.Errorf("replayed callback: profile_error = %q, want google_link_invalid", got)
	}
	if got := e.linkedGoogleID(u.id); got != "" {
		t.Errorf("a replayed state linked %q", got)
	}
}

func TestGoogleLinkCallback_AnotherUsersStateIsRefused(t *testing.T) {
	e := newLinkEnv(t)
	owner := e.user()
	other := e.user()
	googleID := "g_other_" + testutil.UniqueSuffix(t)
	e.googleAccount(googleID, googleID+"@gmail.test", true)

	state := e.startLink(owner)
	rr := e.callback(other.session, state, state)

	if got := settingsError(t, rr); got != "google_link_wrong_user" {
		t.Errorf("profile_error = %q, want google_link_wrong_user", got)
	}
	assertNoSignIn(t, rr)
	if c := responseCookie(rr, linkCookieName); c == nil || c.MaxAge >= 0 {
		t.Error("a refused callback left the link state cookie in place")
	}
	if got := e.linkedGoogleID(other.id); got != "" {
		t.Errorf("the other user was linked to %q", got)
	}
	if got := settingsError(t, e.callback(owner.session, state, state)); got != "google_link_invalid" {
		t.Errorf("owner reusing the refused state: profile_error = %q, want google_link_invalid", got)
	}
	if got := e.linkedGoogleID(owner.id); got != "" {
		t.Errorf("the owner was linked to %q through a spent state", got)
	}
}

func TestGoogleLinkCallback_SignedOutBrowserNeitherLinksNorSignsIn(t *testing.T) {
	e := newLinkEnv(t)
	u := e.user()
	googleID := "g_signedout_" + testutil.UniqueSuffix(t)
	e.googleAccount(googleID, googleID+"@gmail.test", true)

	state := e.startLink(u)
	rr := e.callback("", state, state)

	if got := settingsError(t, rr); got != "google_link_signed_out" {
		t.Errorf("profile_error = %q, want google_link_signed_out", got)
	}
	assertNoSignIn(t, rr)
	if got := e.linkedGoogleID(u.id); got != "" {
		t.Errorf("linked %q without a session", got)
	}
	if got := settingsError(t, e.callback(u.session, state, state)); got != "google_link_invalid" {
		t.Errorf("state reused after a signed-out attempt: profile_error = %q, want google_link_invalid", got)
	}
}

func TestGoogleLinkCallback_StateWithoutItsCookieIsNotLinkMode(t *testing.T) {
	e := newLinkEnv(t)
	u := e.user()
	googleID := "g_nocookie_" + testutil.UniqueSuffix(t)
	e.googleAccount(googleID, googleID+"@gmail.test", true)

	state := e.startLink(u)
	rr := e.callback(u.session, "", state)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400 invalid OAuth state", rr.Code)
	}
	if got := e.linkedGoogleID(u.id); got != "" {
		t.Errorf("linked %q from a browser that did not start the flow", got)
	}
}

func TestGoogleLinkCallback_GoogleIDLinkedToAnotherAccountIsRefused(t *testing.T) {
	e := newLinkEnv(t)
	googleID := "g_held_" + testutil.UniqueSuffix(t)
	holder := e.passwordlessUser(googleID)
	u := e.user()
	e.googleAccount(googleID, googleID+"@gmail.test", true)

	state := e.startLink(u)
	rr := e.callback(u.session, state, state)

	if got := settingsError(t, rr); got != "google_link_taken" {
		t.Errorf("profile_error = %q, want google_link_taken", got)
	}
	assertNoSignIn(t, rr)
	if got := e.linkedGoogleID(u.id); got != "" {
		t.Errorf("linked %q, which another account holds", got)
	}
	if got := e.linkedGoogleID(holder.id); got != googleID {
		t.Errorf("holder's link changed to %q", got)
	}
}

func TestGoogleLinkCallback_UnverifiedGoogleEmailIsRefused(t *testing.T) {
	e := newLinkEnv(t)
	u := e.user()
	googleID := "g_unverified_" + testutil.UniqueSuffix(t)
	e.googleAccount(googleID, u.email, false)

	state := e.startLink(u)
	rr := e.callback(u.session, state, state)

	if got := settingsError(t, rr); got != "google_link_unverified" {
		t.Errorf("profile_error = %q, want google_link_unverified", got)
	}
	if got := e.linkedGoogleID(u.id); got != "" {
		t.Errorf("linked %q with an unverified Google email", got)
	}
}

func TestGoogleLinkCallback_GoogleAccountWithoutAnIDIsRefused(t *testing.T) {
	e := newLinkEnv(t)
	u := e.user()
	e.info = map[string]any{"email": "noid_" + testutil.UniqueSuffix(t) + "@gmail.test", "verified_email": true}

	state := e.startLink(u)
	rr := e.callback(u.session, state, state)

	if got := settingsError(t, rr); got != "google_link_failed" {
		t.Errorf("profile_error = %q, want google_link_failed", got)
	}
	var provider string
	if err := e.db.QueryRowContext(context.Background(), `SELECT oauth_provider FROM users WHERE id = $1`, u.id).Scan(&provider); err != nil || provider != "" {
		t.Errorf("oauth_provider = %q (err %v), want the account left unlinked", provider, err)
	}
}

func TestGoogleLinkCallback_CancelledAtGoogleSpendsTheState(t *testing.T) {
	e := newLinkEnv(t)
	u := e.user()

	state := e.startLink(u)
	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?error=access_denied&state="+url.QueryEscape(state), nil)
	req.AddCookie(&http.Cookie{Name: testCookieName, Value: u.session})
	req.AddCookie(&http.Cookie{Name: linkCookieName, Value: state})
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)

	if got := settingsError(t, rr); got != "google_link_cancelled" {
		t.Errorf("profile_error = %q, want google_link_cancelled", got)
	}
	if got := settingsError(t, e.callback(u.session, state, state)); got != "google_link_invalid" {
		t.Errorf("state reused after cancelling: profile_error = %q, want google_link_invalid", got)
	}
}

// A link flow started in one tab, or a live session, must not change what a Google sign-in does.
func TestGoogleOAuthCallback_LoginModeIgnoresAPendingLinkAndTheSession(t *testing.T) {
	e := newLinkEnv(t)
	u := e.user()
	googleID := "g_pending_" + testutil.UniqueSuffix(t)
	signer := e.passwordlessUser(googleID)
	e.googleAccount(googleID, signer.email, true)
	state := e.startLink(u)

	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?state=s1&code=c1", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "s1"})
	req.AddCookie(&http.Cookie{Name: linkCookieName, Value: state})
	req.AddCookie(&http.Cookie{Name: testCookieName, Value: u.session})
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)

	if !hasAuthCookie(rr) {
		t.Fatalf("login mode: got %d without a session; body: %.300s", rr.Code, rr.Body.String())
	}
	if sub := sessionSubject(t, responseCookie(rr, testCookieName).Value); sub != signer.id {
		t.Errorf("signed in as %d, want the Google-linked account %d", sub, signer.id)
	}
	e.awaitAudit(signer.id, model.AuditActionLogin)
	if got := e.linkedGoogleID(u.id); got != "" {
		t.Errorf("login mode linked the pending account to %q", got)
	}
	e.googleAccount(googleID+"_own", googleID+"_own@gmail.test", true)
	if got := settingsError(t, e.callback(u.session, state, state)); got != "" {
		t.Errorf("the pending link no longer completes: profile_error = %q", got)
	}
	e.awaitAudit(u.id, model.AuditActionOAuthConnect)
}

func TestDisconnectGoogle_AccountWithoutPasswordIsRefused(t *testing.T) {
	e := newLinkEnv(t)
	googleID := "g_only_" + testutil.UniqueSuffix(t)
	u := e.passwordlessUser(googleID)

	if got := settingsError(t, e.disconnect(u, "", "")); got != "google_no_password" {
		t.Errorf("profile_error = %q, want google_no_password", got)
	}
	if got := e.linkedGoogleID(u.id); got != googleID {
		t.Errorf("the account's only sign-in was removed: link is %q", got)
	}
}

func TestDisconnectGoogle_NeedsThePasswordThenUnlinks(t *testing.T) {
	e := newLinkEnv(t)
	u := e.user()
	googleID := "g_disconnect_" + testutil.UniqueSuffix(t)
	e.googleAccount(googleID, googleID+"@gmail.test", true)
	state := e.startLink(u)
	if got := settingsError(t, e.callback(u.session, state, state)); got != "" {
		t.Fatalf("link: profile_error = %q", got)
	}

	if got := settingsError(t, e.disconnect(u, "wrong password", "")); got != "google_reauth_failed" {
		t.Errorf("wrong password: profile_error = %q, want google_reauth_failed", got)
	}
	if got := e.linkedGoogleID(u.id); got != googleID {
		t.Fatalf("a refused disconnect changed the link to %q", got)
	}
	if got := settingsError(t, e.disconnect(u, linkPassword, "")); got != "" {
		t.Fatalf("disconnect: profile_error = %q", got)
	}
	if got := e.linkedGoogleID(u.id); got != "" {
		t.Errorf("still linked to %q", got)
	}
	if !e.awaitAudit(u.id, model.AuditActionOAuthDisconnect) {
		t.Error("no user.oauth.disconnect audit entry")
	} else if got := e.auditOAuthID(u.id, model.AuditActionOAuthDisconnect); got != googleID {
		t.Errorf("disconnect audit entry records oauth_id %q, want %q", got, googleID)
	}
	e.awaitAudit(u.id, model.AuditActionOAuthConnect)
}
