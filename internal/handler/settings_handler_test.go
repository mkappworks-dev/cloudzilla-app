package handler_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newSettingsTestRouter(db *sql.DB) (http.Handler, *service.UserService) {
	cfg := &config.Config{
		Auth: config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName},
	}
	userSvc := service.NewUserService(store.NewUserStore(db), cfg.Auth)
	h := handler.New(&service.Services{User: userSvc}, cfg)
	r := chi.NewRouter()
	r.Post("/settings/notifications", h.UpdateNotificationSettings)
	r.Post("/settings/profile", h.UpdateProfile)
	unauthorized := func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) }
	return middleware.Auth(testJWTSecret, testCookieName, nil, nil, unauthorized)(r), userSvc
}

func TestUpdateNotificationSettings_SavesFormFields(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	router, userSvc := newSettingsTestRouter(db)
	token := makeIssueJWT(t, userID, "testuser_"+suffix)

	post := func(form url.Values) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/settings/notifications", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		req.AddCookie(&http.Cookie{Name: testCookieName, Value: token})
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("want 204, got %d: %s", rr.Code, rr.Body.String())
		}
	}
	saved := func() model.NotificationPrefs {
		t.Helper()
		u, err := userSvc.GetByID(t.Context(), userID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		return model.NotificationPrefs{
			EmailNotifications: u.EmailNotifications,
			EmailDigest:        u.EmailDigest,
			NotifyPRReview:     u.NotifyPRReview,
			NotifyMention:      u.NotifyMention,
		}
	}

	for _, mode := range model.EmailDigestModes {
		post(url.Values{"email_notifications": {"on"}, "email_digest": {mode}, "notify_pr_review": {"on"}, "notify_mention": {"on"}})
		want := model.NotificationPrefs{EmailNotifications: true, EmailDigest: mode, NotifyPRReview: true, NotifyMention: true}
		if got := saved(); got != want {
			t.Errorf("digest %s, all on: prefs = %+v, want %+v", mode, got, want)
		}
	}

	post(url.Values{"email_digest": {"daily"}, "notify_pr_review": {"on"}})
	want := model.NotificationPrefs{EmailDigest: model.EmailDigestDaily, NotifyPRReview: true}
	if got := saved(); got != want {
		t.Errorf("only pr review on: prefs = %+v, want %+v", got, want)
	}

	// Browsers omit unchecked checkboxes but always submit the select.
	post(url.Values{"email_digest": {"weekly"}})
	want = model.NotificationPrefs{EmailDigest: model.EmailDigestWeekly}
	if got := saved(); got != want {
		t.Errorf("after unchecking all: prefs = %+v, want %+v", got, want)
	}

	post(url.Values{"email_digest": {"hourly"}})
	want = model.NotificationPrefs{EmailDigest: model.EmailDigestImmediate}
	if got := saved(); got != want {
		t.Errorf("unknown digest mode: prefs = %+v, want %+v", got, want)
	}
}

func postProfile(t *testing.T, router http.Handler, token string, form url.Values) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/settings/profile", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: testCookieName, Value: token})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d: %s", rr.Code, rr.Body.String())
	}
	return rr.Header().Get("Location")
}

func TestUpdateProfile_IgnoresSubmittedUsername(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	router, userSvc := newSettingsTestRouter(db)
	username := "testuser_" + suffix
	token := makeIssueJWT(t, userID, username)

	loc := postProfile(t, router, token, url.Values{
		"username": {"someone_else_" + suffix},
		"name":     {"Renamed"},
		"email":    {username + "@test.invalid"},
	})
	if loc != "/settings?profile_saved=1#profile" {
		t.Errorf("redirect = %q, want profile_saved", loc)
	}
	u, err := userSvc.GetByID(t.Context(), userID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if u.Username != username {
		t.Errorf("username = %q, want unchanged %q", u.Username, username)
	}
	if u.Name != "Renamed" {
		t.Errorf("name = %q, want %q", u.Name, "Renamed")
	}
}

func TestUpdateProfile_RedirectsWithEmailErrorCodes(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	testutil.SeedUser(t, db, "other_"+suffix)
	router, _ := newSettingsTestRouter(db)
	token := makeIssueJWT(t, userID, "testuser_"+suffix)

	for email, code := range map[string]string{
		"not-an-email": "invalid_email",
		"testuser_other_" + suffix + "@test.invalid": "email_taken",
	} {
		loc := postProfile(t, router, token, url.Values{"name": {"x"}, "email": {email}})
		if want := "/settings?profile_error=" + code + "#profile"; loc != want {
			t.Errorf("email %q: redirect = %q, want %q", email, loc, want)
		}
	}
}

func newEmailPrivacyRouter(t *testing.T) (http.Handler, *sql.DB, string) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "https://git.example.com"},
		Auth: config.AuthConfig{
			JWTSecret:  testJWTSecret,
			JWTExpiry:  24 * time.Hour,
			CookieName: testCookieName,
		},
		Git: config.GitConfig{ReposRoot: reposRoot},
	}
	h := handler.New(service.New(store.New(db), cfg), cfg)

	r := chi.NewRouter()
	r.Get("/settings", h.PageSettings)
	r.Post("/settings/email", h.UpdateEmailSettings)
	r.Post("/{owner}/{repo}/new/{ref}", h.SubmitNewFile)
	r.Post("/settings/profile-readme", h.UpdateProfileReadme)
	unauthorized := func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) }
	return middleware.Auth(testJWTSecret, testCookieName, nil, nil, unauthorized)(r), db, reposRoot
}

func postForm(t *testing.T, router http.Handler, token, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

func saveEmailSettings(t *testing.T, router http.Handler, token string, form url.Values) {
	t.Helper()
	rr := postForm(t, router, token, "/settings/email", form)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/settings#email" {
		t.Fatalf("save email settings: want 303 to /settings#email, got %d %q: %s",
			rr.Code, rr.Header().Get("Location"), rr.Body.String())
	}
}

func keepEmailPrivate(t *testing.T, db *sql.DB, userID int64) bool {
	t.Helper()
	var keep bool
	if err := db.QueryRowContext(context.Background(),
		`SELECT keep_email_private FROM users WHERE id = $1`, userID).Scan(&keep); err != nil {
		t.Fatalf("read keep_email_private: %v", err)
	}
	return keep
}

func TestUpdateEmailSettings_PersistsToggle(t *testing.T) {
	router, db, _ := newEmailPrivacyRouter(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	username := "testuser_" + suffix
	token := makeIssueJWT(t, userID, username)

	saveEmailSettings(t, router, token, url.Values{})
	if keepEmailPrivate(t, db, userID) {
		t.Error("unchecked box must turn the setting off")
	}

	saveEmailSettings(t, router, token, url.Values{"keep_email_private": {"on"}})
	if !keepEmailPrivate(t, db, userID) {
		t.Error("checked box must turn the setting on")
	}

	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /settings: want 200, got %d", rr.Code)
	}
	noreply := fmt.Sprintf("%d+%s@users.noreply.git.example.com", userID, username)
	if !strings.Contains(rr.Body.String(), noreply) {
		t.Errorf("settings page must show the noreply address %q", noreply)
	}
}
