package handler_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestPageLoginSubmit_WrongPassword_KeepsEmailAndMarksPassword(t *testing.T) {
	db := testutil.OpenTestDB(t)
	_, email := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "correctpassword")
	form := url.Values{"email": {email}, "password": {"wrongpassword"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h := newAuthHandler(db)
	h.Services.SSO = service.NewSSOService(store.NewSSOStore(db), store.NewUserStore(db), h.Cfg.Auth, h.Services.SiteSetting)
	h.Services.PasswordReset = service.NewPasswordResetService(store.NewPasswordResetStore(db), store.NewUserStore(db), h.Services.Reauth, service.NewEmailService(config.SMTPConfig{}), "")

	h.PageLoginSubmit(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, `value="`+email+`"`) {
		t.Errorf("a failed login must keep the email; body:\n%s", body)
	}
	for _, want := range []string{"Invalid credentials", `aria-invalid="true"`, `aria-describedby="password-error"`} {
		if !strings.Contains(body, want) {
			t.Errorf("want %q on the password field; body:\n%s", want, body)
		}
	}
}

// The browser validates these too, but a POST without it must not slip through.
func TestPageRegisterSubmit_ServerChecksEmailAndPassword(t *testing.T) {
	for _, tc := range []struct {
		name, email, password, want, field string
	}{
		{"malformed email", "not-an-email", "password123", "Enter a valid email address.", "email"},
		{"display name", "Alice <alice@test.invalid>", "password123", "Enter a valid email address.", "email"},
		{"short password", "short@test.invalid", "12345", "Password must be at least 8 characters.", "password"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			enableRegistration(t, db)
			suffix := testutil.UniqueSuffix(t)
			form := url.Values{"username": {"reg_" + suffix}, "email": {tc.email}, "password": {tc.password}}
			req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rr := httptest.NewRecorder()

			newAuthHandler(db).PageRegisterSubmit(rr, req)

			body := rr.Body.String()
			for _, want := range []string{tc.want, `id="` + tc.field + `-error"`, `aria-invalid="true"`, `value="reg_` + suffix + `"`} {
				if !strings.Contains(body, want) {
					t.Errorf("want %q in the re-rendered form; body:\n%s", want, body)
				}
			}
			if n := countUsersWithEmail(t, db, tc.email); n != 0 {
				t.Errorf("an invalid form must create no account; found %d", n)
			}
		})
	}
}
