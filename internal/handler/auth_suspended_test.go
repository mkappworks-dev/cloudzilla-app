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

func TestLogin_Suspended(t *testing.T) {
	db := testutil.OpenTestDB(t)
	userID, email := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "correctpassword")
	testutil.Exec(t, db, `UPDATE users SET suspended_at = NOW() WHERE id = $1`, userID)
	h := newAuthHandler(db)
	h.Services.SSO = service.NewSSOService(store.NewSSOStore(db), store.NewUserStore(db), h.Cfg.Auth, h.Services.SiteSetting)
	h.Services.PasswordReset = service.NewPasswordResetService(store.NewPasswordResetStore(db), store.NewUserStore(db), h.Services.Reauth, service.NewEmailService(config.SMTPConfig{}), "")

	t.Run("api", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(email, "correctpassword"))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		h.Login(rr, req)
		if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "account_suspended") {
			t.Errorf("want 403 account_suspended, got %d %s", rr.Code, rr.Body.String())
		}
	})

	for _, tc := range []struct {
		password      string
		wantSuspended bool
	}{{"correctpassword", true}, {"wrongpassword", false}} {
		t.Run("web/"+tc.password, func(t *testing.T) {
			form := url.Values{"email": {email}, "password": {tc.password}}
			req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rr := httptest.NewRecorder()
			h.PageLoginSubmit(rr, req)
			if got := strings.Contains(rr.Body.String(), "This account is suspended"); got != tc.wantSuspended {
				t.Errorf("suspension message shown = %v, want %v (status %d)", got, tc.wantSuspended, rr.Code)
			}
			for _, c := range rr.Result().Cookies() {
				if c.Name == testCookieName && c.Value != "" {
					t.Error("a suspended user got a session cookie")
				}
			}
		})
	}
}
