package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	gogit "github.com/go-git/go-git/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type accountDeleteEnv struct {
	router http.Handler
	svc    *service.Services
	root   string
}

func newAccountDeleteEnv(t *testing.T) accountDeleteEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	root := t.TempDir()
	cfg := &config.Config{
		Auth: config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName},
		Git:  config.GitConfig{ReposRoot: root},
	}
	svc := service.New(store.New(db), cfg)
	h := handler.New(svc, cfg)
	r := chi.NewRouter()
	r.Post("/settings/delete-account", h.DeleteAccount)
	r.Post("/api/orgs/", h.CreateOrg)
	r.Post("/api/orgs/{org}/repos", h.CreateOrgRepo)
	r.Get("/{owner}/{repo}/info/refs", h.GitInfoRefs)
	unauthorized := func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) }
	return accountDeleteEnv{router: middleware.Auth(testJWTSecret, testCookieName, nil, nil, unauthorized)(r), svc: svc, root: root}
}

func (e accountDeleteEnv) do(t *testing.T, req *http.Request, token string) *httptest.ResponseRecorder {
	t.Helper()
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

func (e accountDeleteEnv) deleteAccount(t *testing.T, userID int64, username string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"confirm_username": {username}}
	req := httptest.NewRequest(http.MethodPost, "/settings/delete-account", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return e.do(t, req, makeIssueJWT(t, userID, username))
}

// The name a deleted account frees can be taken by anyone, as a user or an
// org; what they create under it must not be the old account's data.
func TestDeleteAccount_FreedNameDoesNotExposeOldRepos(t *testing.T) {
	db := testutil.OpenTestDB(t)
	env := newAccountDeleteEnv(t)
	ctx := context.Background()
	aliceSuffix := testutil.UniqueSuffix(t)
	aliceID := testutil.SeedUser(t, db, aliceSuffix)
	alice := "testuser_" + aliceSuffix
	mallorySuffix := testutil.UniqueSuffix(t)
	malloryID := testutil.SeedUser(t, db, mallorySuffix)
	mallory := "testuser_" + mallorySuffix
	malloryToken := makeIssueJWT(t, malloryID, mallory)

	if _, err := env.svc.Repo.Create(ctx, aliceID, alice, "secret", "alice's private notes", true, service.RepoInitOptions{AddREADME: true}); err != nil {
		t.Fatalf("create alice's repo: %v", err)
	}
	if err := env.svc.Code.WikiPageSave(alice, "secret", "Home", "alice's private wiki", service.GitAuthor{Name: "alice", Email: "alice@test.invalid"}, ""); err != nil {
		t.Fatalf("save alice's wiki: %v", err)
	}
	aliceRepo, err := gogit.PlainOpen(filepath.Join(env.root, alice, "secret.git"))
	if err != nil {
		t.Fatalf("open alice's repo: %v", err)
	}
	aliceHead, err := aliceRepo.Head()
	if err != nil {
		t.Fatalf("alice's head: %v", err)
	}

	if rr := env.deleteAccount(t, aliceID, alice); rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/" {
		t.Fatalf("delete account: want 303 to /, got %d to %q", rr.Code, rr.Header().Get("Location"))
	}

	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM organizations WHERE name = $1`, alice) })
	req := httptest.NewRequest(http.MethodPost, "/api/orgs/", strings.NewReader(`{"name":"`+alice+`"}`))
	req.Header.Set("Content-Type", "application/json")
	if rr := env.do(t, req, malloryToken); rr.Code != http.StatusCreated {
		t.Fatalf("create org %q: want 201, got %d: %s", alice, rr.Code, rr.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/orgs/"+alice+"/repos", strings.NewReader(`{"name":"secret","private":true}`))
	req.Header.Set("Content-Type", "application/json")
	createRR := env.do(t, req, malloryToken)

	req = httptest.NewRequest(http.MethodGet, "/"+alice+"/secret/info/refs?service=git-upload-pack", nil)
	if rr := env.do(t, req, malloryToken); strings.Contains(rr.Body.String(), aliceHead.Hash().String()) {
		t.Errorf("git HTTP advertises alice's commit %s to the new owner of the name", aliceHead.Hash())
	}
	if content, found, _ := env.svc.Code.WikiPageGet(alice, "secret", "Home"); found {
		t.Errorf("alice's wiki is served at the reused name: %q", content)
	}
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create repo under the freed name: want 201, got %d: %s", createRR.Code, createRR.Body.String())
	}
	if _, _, err := env.svc.Code.ResolveRef(alice, "secret", ""); !errors.Is(err, service.ErrEmptyRepo) {
		t.Errorf("new repo under the freed name is not empty: %v", err)
	}
}

func TestDeleteAccount_RefusedWhileSoleOrgOwner(t *testing.T) {
	db := testutil.OpenTestDB(t)
	env := newAccountDeleteEnv(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	username := "testuser_" + suffix

	org, err := env.svc.Org.Create(ctx, userID, "testorg_"+suffix, "", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	testutil.DeleteOrgOnCleanup(t, db, org.ID)

	rr := env.deleteAccount(t, userID, username)
	if loc := rr.Header().Get("Location"); rr.Code != http.StatusSeeOther || !strings.Contains(loc, "profile_error=sole_org_owner") {
		t.Errorf("want 303 naming sole_org_owner, got %d to %q", rr.Code, loc)
	}
	if _, err := env.svc.User.GetByID(ctx, userID); err != nil {
		t.Errorf("account deleted despite the refusal: %v", err)
	}
}

func TestDeleteAccount_OrgKeepsServingReposTheUserCreated(t *testing.T) {
	db := testutil.OpenTestDB(t)
	env := newAccountDeleteEnv(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	username := "testuser_" + suffix
	coSuffix := testutil.UniqueSuffix(t)
	coOwnerID := testutil.SeedUser(t, db, coSuffix)
	coOwnerToken := makeIssueJWT(t, coOwnerID, "testuser_"+coSuffix)

	org, err := env.svc.Org.Create(ctx, coOwnerID, "testorg_"+suffix, "", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	testutil.DeleteOrgOnCleanup(t, db, org.ID)
	if err := env.svc.Org.AddMember(ctx, org.ID, coOwnerID, userID, model.OrgRoleOwner); err != nil {
		t.Fatalf("add owner: %v", err)
	}
	if _, err := env.svc.Org.CreateRepo(ctx, org.ID, userID, "orgowned", "", true, service.RepoInitOptions{AddREADME: true}); err != nil {
		t.Fatalf("create org repo: %v", err)
	}
	gitRepo, err := gogit.PlainOpen(filepath.Join(env.root, org.Name, "orgowned.git"))
	if err != nil {
		t.Fatalf("open org repo: %v", err)
	}
	head, err := gitRepo.Head()
	if err != nil {
		t.Fatalf("org repo head: %v", err)
	}

	if rr := env.deleteAccount(t, userID, username); rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/" {
		t.Fatalf("delete account: want 303 to /, got %d to %q", rr.Code, rr.Header().Get("Location"))
	}

	req := httptest.NewRequest(http.MethodGet, "/"+org.Name+"/orgowned/info/refs?service=git-upload-pack", nil)
	if rr := env.do(t, req, coOwnerToken); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), head.Hash().String()) {
		t.Errorf("org repo after its creator's deletion: want git fetch 200 with %s, got %d", head.Hash(), rr.Code)
	}
}
