package handler_test

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

var rawTokenRE = regexp.MustCompile(`czp_[0-9a-f]{64}`)

func TestCreateToken_ShowsTokenOnceWithoutPuttingItInAURL(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID, _ := testutil.SeedUserWithPassword(t, db, suffix, "password1")

	var logs bytes.Buffer
	defaultLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(defaultLogger) })

	cfg := &config.Config{
		Auth: config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: time.Hour, CookieName: testCookieName},
		Git:  config.GitConfig{ReposRoot: t.TempDir()},
	}
	svc := service.New(store.New(db), cfg)
	h := handler.New(svc, cfg)
	r := chi.NewRouter()
	r.Get("/settings", h.PageSettings)
	r.Post("/api/user/tokens", h.CreateToken)
	unauthorized := func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) }
	srv := httptest.NewServer(middleware.Logger(middleware.Auth(testJWTSecret, testCookieName, nil, nil, unauthorized)(r)))
	t.Cleanup(srv.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	srvURL, _ := url.Parse(srv.URL)
	jar.SetCookies(srvURL, []*http.Cookie{{Name: testCookieName, Value: makeIssueJWT(t, userID, "testpw_"+suffix), Path: "/"}})
	var visited []string
	var flash []*http.Cookie
	client := &http.Client{Jar: jar, CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		visited = append(visited, req.URL.String())
		flash = append(flash, req.Response.Cookies()...)
		return nil
	}}
	body := func(resp *http.Response) string {
		t.Helper()
		b, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: want 200, got %d: %s", resp.Request.URL, resp.StatusCode, b)
		}
		return string(b)
	}

	resp, err := client.PostForm(srv.URL+"/api/user/tokens", url.Values{"name": {"ci " + suffix}, "scopes": {"repo:read"}, "password": {"password1"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Request.URL.Path != "/settings" {
		t.Fatalf("token creation landed on %s, want /settings", resp.Request.URL)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("page showing the new token has Cache-Control %q, want no-store", got)
	}
	tokens := htmlSection(t, body(resp), "tokens")
	raw := rawTokenRE.FindString(tokens)
	if raw == "" {
		t.Fatalf("settings page after creation does not show the new token:\n%s", tokens)
	}
	if _, _, err := svc.AccessToken.Validate(t.Context(), raw); err != nil {
		t.Errorf("shown token %q is not a valid token: %v", raw, err)
	}
	for _, u := range visited {
		if strings.Contains(u, raw) {
			t.Errorf("redirect URL carries the raw token: %s", u)
		}
	}
	handedOver := false
	for _, c := range flash {
		if c.Value != raw {
			continue
		}
		handedOver = true
		if !c.HttpOnly || c.Path != "/settings" {
			t.Errorf("token cookie HttpOnly=%v Path=%q, want HttpOnly on /settings", c.HttpOnly, c.Path)
		}
	}
	if !handedOver {
		t.Error("the redirect did not hand the token over in a cookie")
	}

	resp, err = client.Get(srv.URL + "/settings")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(htmlSection(t, body(resp), "tokens"), raw) {
		t.Error("a second visit to /settings shows the token again")
	}
	if strings.Contains(logs.String(), raw) {
		t.Errorf("server logs contain the raw token:\n%s", logs.String())
	}
}
