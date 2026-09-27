package handler_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const signupBaseURL = "https://cz.test"

type sentSignupMail struct{ kind, to, url string }

// fakeSignupMailer reports mail on a channel because /register sends it from a goroutine.
type fakeSignupMailer struct{ sent chan sentSignupMail }

func newFakeSignupMailer() *fakeSignupMailer {
	return &fakeSignupMailer{sent: make(chan sentSignupMail, 16)}
}

func (m *fakeSignupMailer) Enabled() bool { return true }

func (m *fakeSignupMailer) SendSignupLink(to, link string) error {
	m.sent <- sentSignupMail{"link", to, link}
	return nil
}

func (m *fakeSignupMailer) SendAccountExists(to, loginURL string) error {
	m.sent <- sentSignupMail{"exists", to, loginURL}
	return nil
}

func (m *fakeSignupMailer) next(t *testing.T) sentSignupMail {
	t.Helper()
	select {
	case s := <-m.sent:
		return s
	case <-time.After(5 * time.Second):
		t.Fatal("no signup email was sent")
		return sentSignupMail{}
	}
}

func (m *fakeSignupMailer) assertNoneSent(t *testing.T) {
	t.Helper()
	select {
	case s := <-m.sent:
		t.Errorf("want no email, got %+v", s)
	case <-time.After(200 * time.Millisecond):
	}
}

func newSignupHandler(db *sql.DB, mailer service.SignupMailer) *handler.Handler {
	h := newAuthHandler(db)
	h.Services.Signup = service.NewSignupService(store.NewSignupTokenStore(db), store.NewUserStore(db), mailer, signupBaseURL)
	return h
}

func signupRouter(h *handler.Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/register", h.PageRegister)
	r.Post("/register", h.PageRegisterSubmit)
	r.Get("/register/complete/{token}", h.PageRegisterComplete)
	r.Post("/register/complete/{token}", h.PageRegisterCompleteSubmit)
	return r
}

func serveSignup(h *handler.Handler, method, path string, form url.Values) *httptest.ResponseRecorder {
	var req *http.Request
	if form == nil {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rr := httptest.NewRecorder()
	signupRouter(h).ServeHTTP(rr, req)
	return rr
}

func cleanupSignup(t *testing.T, db *sql.DB, email string) {
	t.Helper()
	t.Cleanup(func() {
		testutil.Exec(t, db, `DELETE FROM signup_tokens WHERE lower(email) = lower($1)`, email)
		testutil.Exec(t, db, `DELETE FROM users WHERE lower(email) = lower($1) AND username LIKE 'signup\_%'`, email)
	})
}

// requestSignupLink submits /register for email and returns the token from the emailed link.
func requestSignupLink(t *testing.T, db *sql.DB, h *handler.Handler, mailer *fakeSignupMailer, email string) string {
	t.Helper()
	enableRegistration(t, db)
	serveSignup(h, http.MethodPost, "/register", url.Values{"email": {email}})
	m := mailer.next(t)
	const prefix = signupBaseURL + "/register/complete/"
	if m.kind != "link" || !strings.HasPrefix(m.url, prefix) {
		t.Fatalf("want a signup link email, got %+v", m)
	}
	return strings.TrimPrefix(m.url, prefix)
}

func TestPageRegister_SignupEnabled_AsksOnlyForEmail(t *testing.T) {
	db := testutil.OpenTestDB(t)
	enableRegistration(t, db)

	body := serveSignup(newSignupHandler(db, newFakeSignupMailer()), http.MethodGet, "/register", nil).Body.String()

	if !strings.Contains(body, `name="email"`) || strings.Contains(body, `name="password"`) {
		t.Errorf("want the email-only form:\n%s", body)
	}
}

// The heart of the design: nothing in the response depends on whether the
// address has an account.
func TestPageRegisterSubmit_SignupEnabled_SameResponseForNewAndExistingEmail(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	testutil.SeedUser(t, db, suffix)
	existing := "TestUser_" + suffix + "@Test.Invalid"
	fresh := "signup_" + suffix + "@test.invalid"
	cleanupSignup(t, db, existing)
	cleanupSignup(t, db, fresh)
	enableRegistration(t, db)
	mailer := newFakeSignupMailer()
	h := newSignupHandler(db, mailer)

	newRR := serveSignup(h, http.MethodPost, "/register", url.Values{"email": {fresh}})
	existingRR := serveSignup(h, http.MethodPost, "/register", url.Values{"email": {existing}})

	if newRR.Code != existingRR.Code || newRR.Body.String() != existingRR.Body.String() {
		t.Errorf("responses differ:\nnew (%d):\n%s\nexisting (%d):\n%s", newRR.Code, newRR.Body, existingRR.Code, existingRR.Body)
	}
	if !strings.Contains(newRR.Body.String(), "Check your inbox") {
		t.Errorf("want the check-inbox page:\n%s", newRR.Body)
	}
	kinds := map[string]string{}
	for range 2 {
		m := mailer.next(t)
		kinds[strings.ToLower(m.to)] = m.kind
	}
	if kinds[strings.ToLower(fresh)] != "link" || kinds[strings.ToLower(existing)] != "exists" {
		t.Errorf("want a link for the new address and an account-exists mail for the existing one, got %v", kinds)
	}
}

func TestPageRegisterSubmit_SignupEnabled_InvalidEmail_FormErrorAndNoMail(t *testing.T) {
	tooLong := strings.Repeat("a", 64) + "@" + strings.Repeat("b", 177) + ".test.invalid"
	for _, email := range []string{"", "not-an-email", "Alice <alice@test.invalid>", "a@test.invalid\r\nBcc: b@test.invalid", tooLong} {
		t.Run(email, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			enableRegistration(t, db)
			mailer := newFakeSignupMailer()

			body := serveSignup(newSignupHandler(db, mailer), http.MethodPost, "/register", url.Values{"email": {email}}).Body.String()

			if !strings.Contains(body, "Enter a valid email address") {
				t.Errorf("want the invalid-email error:\n%s", body)
			}
			mailer.assertNoneSent(t)
		})
	}
}

func completeForm(username string) url.Values {
	return url.Values{"username": {username}, "password": {"password123"}}
}

func TestPageRegisterComplete_UsableLink_ShowsFormWithEmail(t *testing.T) {
	db := testutil.OpenTestDB(t)
	email := "signup_" + testutil.UniqueSuffix(t) + "@test.invalid"
	cleanupSignup(t, db, email)
	mailer := newFakeSignupMailer()
	h := newSignupHandler(db, mailer)
	token := requestSignupLink(t, db, h, mailer, email)

	body := serveSignup(h, http.MethodGet, "/register/complete/"+token, nil).Body.String()

	if !strings.Contains(body, `value="`+email+`"`) || !strings.Contains(body, `name="username"`) {
		t.Errorf("want the completion form for %s:\n%s", email, body)
	}
}

func assertInvalidSignupLinkPage(t *testing.T, body, hiddenEmail string) {
	t.Helper()
	if !strings.Contains(body, "This link is no longer valid") {
		t.Errorf("want the invalid-link page:\n%s", body)
	}
	if strings.Contains(body, `name="username"`) {
		t.Error("an unusable link must not render the form")
	}
	if hiddenEmail != "" && strings.Contains(strings.ToLower(body), strings.ToLower(hiddenEmail)) {
		t.Errorf("an unusable link must not show its email:\n%s", body)
	}
}

func TestPageRegisterComplete_UnusableLink_GenericPage(t *testing.T) {
	cases := []struct {
		name  string
		spoil func(t *testing.T, db *sql.DB, suffix, email string)
	}{
		{"used", func(t *testing.T, db *sql.DB, _, email string) {
			testutil.Exec(t, db, `UPDATE signup_tokens SET used_at = NOW() WHERE lower(email) = lower($1)`, email)
		}},
		{"expired", func(t *testing.T, db *sql.DB, _, email string) {
			testutil.Exec(t, db, `UPDATE signup_tokens SET expires_at = NOW() - INTERVAL '1 minute' WHERE lower(email) = lower($1)`, email)
		}},
		{"email registered since", func(t *testing.T, db *sql.DB, suffix, _ string) {
			testutil.SeedUser(t, db, suffix)
		}},
	}
	for _, tc := range cases {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			t.Run(tc.name+"/"+method, func(t *testing.T) {
				db := testutil.OpenTestDB(t)
				suffix := testutil.UniqueSuffix(t)
				email := "testuser_" + suffix + "@test.invalid"
				cleanupSignup(t, db, email)
				mailer := newFakeSignupMailer()
				h := newSignupHandler(db, mailer)
				token := requestSignupLink(t, db, h, mailer, email)
				tc.spoil(t, db, suffix, email)

				var form url.Values
				if method == http.MethodPost {
					form = completeForm("signup_" + suffix)
				}
				body := serveSignup(h, method, "/register/complete/"+token, form).Body.String()

				assertInvalidSignupLinkPage(t, body, email)
			})
		}
	}
}

func TestPageRegisterComplete_UnknownToken_GenericPage(t *testing.T) {
	db := testutil.OpenTestDB(t)
	enableRegistration(t, db)

	body := serveSignup(newSignupHandler(db, newFakeSignupMailer()), http.MethodGet, "/register/complete/nope"+testutil.UniqueSuffix(t), nil).Body.String()

	assertInvalidSignupLinkPage(t, body, "")
}

func TestPageRegisterCompleteSubmit_CreatesAccountAndSignsIn(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	email := "signup_" + suffix + "@test.invalid"
	cleanupSignup(t, db, email)
	mailer := newFakeSignupMailer()
	h := newSignupHandler(db, mailer)
	token := requestSignupLink(t, db, h, mailer, email)

	rr := serveSignup(h, http.MethodPost, "/register/complete/"+token, completeForm("signup_"+suffix))

	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/" {
		t.Fatalf("want 303 to /, got %d %q: %s", rr.Code, rr.Header().Get("Location"), rr.Body)
	}
	var signedIn bool
	for _, c := range rr.Result().Cookies() {
		signedIn = signedIn || (c.Name == testCookieName && c.Value != "")
	}
	if !signedIn {
		t.Error("completing signup must sign the user in")
	}
	var invited bool
	if err := db.QueryRowContext(context.Background(), `SELECT is_invited FROM users WHERE username = $1 AND email = $2`, "signup_"+suffix, email).Scan(&invited); err != nil {
		t.Fatalf("read user: %v", err)
	}
	if invited {
		t.Error("a self-registered account must not be marked invited")
	}

	replay := serveSignup(h, http.MethodPost, "/register/complete/"+token, completeForm("signup2_"+suffix))
	assertInvalidSignupLinkPage(t, replay.Body.String(), email)
}

func TestPageRegisterCompleteSubmit_UsernameTaken_LinkStaysUsable(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	testutil.SeedUser(t, db, suffix)
	email := "signup_" + suffix + "@test.invalid"
	cleanupSignup(t, db, email)
	mailer := newFakeSignupMailer()
	h := newSignupHandler(db, mailer)
	token := requestSignupLink(t, db, h, mailer, email)

	body := serveSignup(h, http.MethodPost, "/register/complete/"+token, completeForm("testuser_"+suffix)).Body.String()

	assertNoDBErrorText(t, body)
	if !strings.Contains(body, "username is already taken") {
		t.Errorf("want the username-taken message:\n%s", body)
	}
	if rr := serveSignup(h, http.MethodGet, "/register/complete/"+token, nil); !strings.Contains(rr.Body.String(), `name="username"`) {
		t.Error("a failed create must leave the link usable")
	}
}

func TestPageRegisterCompleteSubmit_PasswordTooLong_NoAccountAndLinkStaysUsable(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	email := "signup_" + suffix + "@test.invalid"
	cleanupSignup(t, db, email)
	mailer := newFakeSignupMailer()
	h := newSignupHandler(db, mailer)
	token := requestSignupLink(t, db, h, mailer, email)

	body := serveSignup(h, http.MethodPost, "/register/complete/"+token, url.Values{
		"username": {"signup_" + suffix}, "password": {strings.Repeat("p", 73)},
	}).Body.String()

	if !strings.Contains(body, "Password is too long (maximum 72 bytes)") {
		t.Errorf("want the password-too-long message:\n%s", body)
	}
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM users WHERE username = $1`, "signup_"+suffix).Scan(&n); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n != 0 {
		t.Error("an over-long password must not create an account")
	}
	if rr := serveSignup(h, http.MethodGet, "/register/complete/"+token, nil); !strings.Contains(rr.Body.String(), `name="username"`) {
		t.Error("a rejected password must leave the link usable")
	}
}

func TestPageRegisterCompleteSubmit_ConcurrentSubmits_OneAccount(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	email := "signup_" + suffix + "@test.invalid"
	cleanupSignup(t, db, email)
	mailer := newFakeSignupMailer()
	h := newSignupHandler(db, mailer)
	token := requestSignupLink(t, db, h, mailer, email)

	const submits = 5
	results := make(chan *httptest.ResponseRecorder, submits)
	var wg sync.WaitGroup
	for i := range submits {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- serveSignup(h, http.MethodPost, "/register/complete/"+token, completeForm(fmt.Sprintf("signup_%s_%d", suffix, i)))
		}()
	}
	wg.Wait()
	close(results)

	created := 0
	for rr := range results {
		if rr.Code == http.StatusSeeOther {
			created++
			continue
		}
		assertInvalidSignupLinkPage(t, rr.Body.String(), "")
	}
	if created != 1 {
		t.Errorf("want exactly 1 account, got %d", created)
	}
}

func TestPageRegisterComplete_RegistrationClosed_RedirectsToLogin(t *testing.T) {
	db := testutil.OpenTestDB(t)
	email := "signup_" + testutil.UniqueSuffix(t) + "@test.invalid"
	cleanupSignup(t, db, email)
	mailer := newFakeSignupMailer()
	h := newSignupHandler(db, mailer)
	token := requestSignupLink(t, db, h, mailer, email)
	setAllowRegistration(t, db, "false")

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		rr := serveSignup(newSignupHandler(db, mailer), method, "/register/complete/"+token, completeForm("signup_x"))
		if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/login" {
			t.Errorf("%s: want 303 to /login, got %d %q", method, rr.Code, rr.Header().Get("Location"))
		}
	}
}
