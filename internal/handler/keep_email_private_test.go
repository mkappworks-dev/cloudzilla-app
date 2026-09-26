package handler_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newKeepEmailHandler(t *testing.T) (http.Handler, *sql.DB, string) {
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

func keepEmailPrivate(t *testing.T, db *sql.DB, userID int64) bool {
	t.Helper()
	var keep bool
	if err := db.QueryRowContext(context.Background(),
		`SELECT keep_email_private FROM users WHERE id = $1`, userID).Scan(&keep); err != nil {
		t.Fatalf("read keep_email_private: %v", err)
	}
	return keep
}

func TestSubmitNewFile_AuthorEmailFollowsKeepEmailPrivate(t *testing.T) {
	router, db, reposRoot := newKeepEmailHandler(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	owner := "testuser_" + suffix
	repoName := "testrepo_" + suffix
	testutil.SeedRepo(t, db, userID, owner, suffix)
	gitRepo, err := gogit.PlainInit(filepath.Join(reposRoot, owner, repoName+".git"), true)
	if err != nil {
		t.Fatalf("init bare repo: %v", err)
	}
	token := makeIssueJWT(t, userID, owner)

	headAuthor := func(path string) string {
		t.Helper()
		rr := postForm(t, router, token, "/"+owner+"/"+repoName+"/new/main",
			url.Values{"path": {path}, "content": {"hello\n"}, "message": {"Add " + path}})
		if rr.Code != http.StatusSeeOther {
			t.Fatalf("commit %s: want 303, got %d: %s", path, rr.Code, rr.Body.String())
		}
		ref, err := gitRepo.Reference(plumbing.NewBranchReferenceName("main"), true)
		if err != nil {
			t.Fatalf("resolve main: %v", err)
		}
		c, err := gitRepo.CommitObject(ref.Hash())
		if err != nil {
			t.Fatalf("load head commit: %v", err)
		}
		if c.Author.Name != owner {
			t.Errorf("author name = %q, want %q", c.Author.Name, owner)
		}
		return c.Author.Email
	}

	wantNoreply := fmt.Sprintf("%d+%s@users.noreply.git.example.com", userID, owner)
	if got := headAuthor("private.txt"); got != wantNoreply {
		t.Errorf("setting on: author email = %q, want %q", got, wantNoreply)
	}

	if _, err := db.ExecContext(context.Background(),
		`UPDATE users SET keep_email_private = FALSE WHERE id = $1`, userID); err != nil {
		t.Fatalf("turn setting off: %v", err)
	}
	if got, want := headAuthor("public.txt"), owner+"@test.invalid"; got != want {
		t.Errorf("setting off: author email = %q, want %q", got, want)
	}
}

func TestUpdateEmailSettings_PersistsToggle(t *testing.T) {
	router, db, _ := newKeepEmailHandler(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	username := "testuser_" + suffix
	token := makeIssueJWT(t, userID, username)

	save := func(form url.Values) {
		t.Helper()
		rr := postForm(t, router, token, "/settings/email", form)
		if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/settings#email" {
			t.Fatalf("save: want 303 to /settings#email, got %d %q: %s",
				rr.Code, rr.Header().Get("Location"), rr.Body.String())
		}
	}

	save(url.Values{})
	if keepEmailPrivate(t, db, userID) {
		t.Error("unchecked box must turn the setting off")
	}

	save(url.Values{"keep_email_private": {"on"}})
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
