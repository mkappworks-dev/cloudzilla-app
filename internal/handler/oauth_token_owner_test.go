package handler_test

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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

// oauthAppToken authorizes an app for userID and returns the bearer token the
// app receives.
func oauthAppToken(t *testing.T, svc *service.Services, appOwnerID, userID int64) string {
	t.Helper()
	ctx := context.Background()
	app, secret, err := svc.OAuthApp.CreateApp(ctx, appOwnerID, "app "+testutil.UniqueSuffix(t), "", "", nil)
	if err != nil {
		t.Fatalf("create oauth app: %v", err)
	}
	code, err := svc.OAuthApp.Authorize(ctx, app.ID, userID, "", []string{"read"}, app)
	if err != nil {
		t.Fatalf("authorize oauth app: %v", err)
	}
	token, err := svc.OAuthApp.ExchangeCode(ctx, app.ClientID, secret, code)
	if err != nil {
		t.Fatalf("exchange oauth code: %v", err)
	}
	return token
}

// An OAuth-app token names its user by ID alone. Repos it creates must still
// land in that user's directory: at the top of the repos root they would sit in
// the owner namespace, where an org named <name>.git keeps its repos.
func TestOAuthToken_ForkAndTemplateCreateUnderTheUser(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	root := t.TempDir()
	cfg := &config.Config{
		Auth: config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: time.Hour, CookieName: testCookieName},
		Git:  config.GitConfig{ReposRoot: root},
	}
	svc := service.New(store.New(db), cfg)
	h := handler.New(svc, cfg)
	r := chi.NewRouter()
	r.Post("/api/repos/from-template", h.CreateFromTemplate)
	r.Post("/api/repos/{owner}/{repo}/fork", h.ForkRepo)
	unauthorized := func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) }
	router := middleware.Auth(testJWTSecret, testCookieName, svc.AccessToken, svc.OAuthApp, unauthorized)(r)

	ownerSuffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, ownerSuffix)
	owner := "testuser_" + ownerSuffix
	userSuffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, userSuffix)
	user := "testuser_" + userSuffix

	tmpl, err := svc.Repo.Create(ctx, ownerID, owner, "tmpl", "", false, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	if err := svc.Repo.SetTemplate(ctx, tmpl.ID, ownerID, true); err != nil {
		t.Fatalf("mark template: %v", err)
	}
	token := oauthAppToken(t, svc, ownerID, userID)

	post := func(path string, form url.Values) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(httptest.NewRecorder(), req)
	}
	post("/api/repos/"+owner+"/tmpl/fork", nil)
	post("/api/repos/from-template", url.Values{"template_repo_id": {strconv.FormatInt(tmpl.ID, 10)}, "name": {"fromtmpl"}})

	for _, name := range []string{"tmpl", "fromtmpl"} {
		if _, err := svc.Repo.Get(ctx, user, name); err != nil {
			t.Errorf("%s not created under %s: %v", name, user, err)
		}
		for _, dir := range []string{name + ".git", name + ".wiki.git"} {
			if _, err := os.Lstat(filepath.Join(root, dir)); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("%s claimed at the top of the repos root (lstat: %v)", dir, err)
			}
		}
	}
	var stray int
	if err := db.QueryRow(`SELECT COUNT(*) FROM repositories WHERE owner_id = $1 AND owner_name <> $2`, userID, user).Scan(&stray); err != nil {
		t.Fatalf("count repos: %v", err)
	}
	if stray != 0 {
		t.Errorf("%d of the user's repos carry another owner name", stray)
	}
}
