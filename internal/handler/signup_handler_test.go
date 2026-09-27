package handler_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
//
//nolint:unused // consumed by the register-complete tests landing next
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
	for _, email := range []string{"", "not-an-email", "Alice <alice@test.invalid>", "a@test.invalid\r\nBcc: b@test.invalid"} {
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
