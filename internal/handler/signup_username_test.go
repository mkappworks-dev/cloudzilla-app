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
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newSignupRouter(t *testing.T, db *sql.DB) (http.Handler, *service.Services) {
	t.Helper()
	cfg := &config.Config{Auth: config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: time.Hour, CookieName: testCookieName}}
	svc := service.New(store.New(db), cfg)
	h := handler.New(svc, cfg)
	r := chi.NewRouter()
	r.Post("/register", h.PageRegisterSubmit)
	r.Post("/invite/{token}", h.PageInviteSubmit)
	return r, svc
}

func postSignupForm(router http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

func assertNoUserNamed(t *testing.T, db *sql.DB, svc *service.Services, name string) {
	t.Helper()
	if u, err := svc.User.GetByUsername(context.Background(), name); err == nil {
		t.Errorf("user %q was created", name)
		testutil.DeleteUsers(t, db, u.ID)
	}
}

func TestPageRegisterSubmit_RejectsInvalidUsername(t *testing.T) {
	db := testutil.OpenTestDB(t)
	router, svc := newSignupRouter(t, db)

	for _, name := range testutil.HostileNames {
		suffix := testutil.UniqueSuffix(t)
		rr := postSignupForm(router, "/register", url.Values{
			"username": {name},
			"email":    {"reg_" + suffix + "@test.invalid"},
			"password": {"password123"},
		})
		if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), service.ErrInvalidUsername.Error()) {
			t.Errorf("register %q: got %d, want the form re-rendered with the username error", name, rr.Code)
		}
		assertNoUserNamed(t, db, svc, name)
	}
}

func TestPageInviteSubmit_RejectsInvalidUsername(t *testing.T) {
	db := testutil.OpenTestDB(t)
	adminID := testutil.SeedSuperadmin(t, db, testutil.UniqueSuffix(t))
	router, svc := newSignupRouter(t, db)
	inv, err := svc.Invitation.Create(context.Background(), adminID, "inv_"+testutil.UniqueSuffix(t)+"@test.invalid")
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM invitations WHERE id = $1`, inv.ID) })

	for _, name := range testutil.HostileNames {
		rr := postSignupForm(router, "/invite/"+inv.Token, url.Values{"username": {name}, "password": {"password123"}})
		if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), service.ErrInvalidUsername.Error()) {
			t.Errorf("invite %q: got %d, want the form re-rendered with the username error", name, rr.Code)
		}
		assertNoUserNamed(t, db, svc, name)
	}
}
