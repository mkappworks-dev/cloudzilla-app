package handler_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

type forkPullFixture struct {
	router           http.Handler
	upOwner, upRepo  string
	upID             int64
	forkerID         int64
	db               *sql.DB
	forkID           int64
	forker, forkRepo string
	forkSHA          plumbing.Hash
}

// openForkPull forks an upstream, adds a commit on the fork's fix-x and opens the pull request through the API.
func openForkPull(t *testing.T) forkPullFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	root := t.TempDir()
	suffix := testutil.UniqueSuffix(t)
	upID := testutil.SeedUser(t, db, suffix)
	f := forkPullFixture{upOwner: "testuser_" + suffix, upRepo: "testrepo_" + suffix, upID: upID}
	testutil.SeedRepo(t, db, upID, f.upOwner, suffix)
	up := testutil.InitBareRepo(t, filepath.Join(root, f.upOwner, f.upRepo+".git"))
	main := testutil.WriteCommit(t, up.Storer, "init")
	if err := up.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), main)); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Auth: config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName},
		Git:  config.GitConfig{ReposRoot: root},
	}
	userSvc := service.NewUserService(store.NewUserStore(db), cfg.Auth)
	repoSvc := service.NewRepoService(store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db), nil, nil, cfg.Git)
	code := service.NewCodeService(cfg.Git)
	svc := &service.Services{
		User:    userSvc,
		Repo:    repoSvc,
		Pull:    service.NewPullService(store.NewPullStore(db), store.NewRepoStore(db), repoSvc).WithCIDeps(code, nil, nil, nil, nil, nil),
		Webhook: service.NewWebhookService(store.NewWebhookStore(db), config.WebhookConfig{}),
		Event:   service.NewEventService(store.NewEventStore(db), store.NewUserStore(db), store.NewRepoStore(db)),
		Code:    code,
		Assignee: service.NewAssigneeService(store.NewAssigneeStore(db), store.NewRepoStore(db), store.NewIssueStore(db),
			store.NewPullStore(db), store.NewUserStore(db)),
		AuditLog:    service.NewAuditService(store.NewAuditLogStore(db)),
		SiteSetting: service.NewSiteSettingService(store.NewSiteSettingStore(db), store.NewUserStore(db)),
	}
	h := handler.New(svc, cfg)
	r := chi.NewRouter()
	r.Post("/api/repos/{owner}/{repo}/pulls", h.CreatePull)
	r.Post("/api/repos/{owner}/{repo}/pulls/{number}/update-branch", h.UpdatePullBranch)
	r.Get("/api/repos/{owner}/{repo}/pulls/{number}", h.GetPull)
	f.router = middleware.Auth(testJWTSecret, testCookieName, nil, nil, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) })(r)

	f.forkerID = testutil.SeedUser(t, db, suffix+"_f")
	f.forker = "testuser_" + suffix + "_f"
	fork, err := repoSvc.Fork(context.Background(), f.upOwner, f.upRepo, f.forkerID, f.forker, service.ForkOptions{})
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	f.forkRepo = fork.Name
	f.db = db
	f.forkID = fork.ID
	forkGit, err := gogit.PlainOpen(filepath.Join(root, f.forker, fork.Name+".git"))
	if err != nil {
		t.Fatal(err)
	}
	f.forkSHA = testutil.WriteCommit(t, forkGit.Storer, "fork change", main)
	if err := forkGit.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName("fix-x"), f.forkSHA)); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f forkPullFixture) post(t *testing.T, path string, userID int64, username string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeIssueJWT(t, userID, username))
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)
	return rr
}

func (f forkPullFixture) open(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	return f.post(t, "/api/repos/"+f.upOwner+"/"+f.upRepo+"/pulls", f.forkerID, f.forker, map[string]any{
		"title": "Fix x", "head_branch": "fix-x", "base_branch": "main", "head_repo": f.forker + "/" + f.forkRepo,
	})
}

func TestCreatePull_FromForkHead_201(t *testing.T) {
	f := openForkPull(t)

	rr := f.open(t)

	if rr.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["head_repo"] != f.forker+"/"+f.forkRepo {
		t.Errorf("head_repo = %v, want %s/%s", got["head_repo"], f.forker, f.forkRepo)
	}
	if got["head_sha"] != f.forkSHA.String() {
		t.Errorf("head_sha = %v, want %s", got["head_sha"], f.forkSHA)
	}
}

func TestUpdatePullBranch_ForkPull_422(t *testing.T) {
	f := openForkPull(t)
	if rr := f.open(t); rr.Code != http.StatusCreated {
		t.Fatalf("open: %d %s", rr.Code, rr.Body.String())
	}

	rr := f.post(t, "/api/repos/"+f.upOwner+"/"+f.upRepo+"/pulls/1/update-branch", f.upID, f.upOwner, nil)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreatePull_HeadOutsideForkTree_422(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	upID := testutil.SeedUser(t, db, suffix)
	upOwner, upRepo := "testuser_"+suffix, "testrepo_"+suffix
	testutil.SeedRepo(t, db, upID, upOwner, suffix)
	otherID := testutil.SeedUser(t, db, suffix+"_o")
	other := "testuser_" + suffix + "_o"
	testutil.SeedRepo(t, db, otherID, other, suffix+"_o")

	router := pullRouterWithAuth(newPullHandler(db))
	body, _ := json.Marshal(map[string]any{
		"title": "Fix x", "head_branch": "main", "base_branch": "main", "head_repo": other + "/testrepo_" + suffix + "_o",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/"+upOwner+"/"+upRepo+"/pulls", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeIssueJWT(t, otherID, other))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestGetPull_PrivateForkHead_HiddenFromAReaderOfTheBase(t *testing.T) {
	f := openForkPull(t)
	if rr := f.open(t); rr.Code != http.StatusCreated {
		t.Fatalf("open: %d %s", rr.Code, rr.Body.String())
	}
	testutil.Exec(t, f.db, `UPDATE repositories SET private = true WHERE id = $1`, f.forkID)
	get := func(userID int64, username string) map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/repos/"+f.upOwner+"/"+f.upRepo+"/pulls/1", nil)
		req.Header.Set("Authorization", "Bearer "+makeIssueJWT(t, userID, username))
		rr := httptest.NewRecorder()
		f.router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("get: %d %s", rr.Code, rr.Body.String())
		}
		var got map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		return got
	}

	reader := get(f.upID, f.upOwner)
	author := get(f.forkerID, f.forker)

	if reader["head_repo"] != nil || reader["head_repo_hidden"] != true {
		t.Errorf("a reader of the base only: head_repo = %v, head_repo_hidden = %v; want the name withheld", reader["head_repo"], reader["head_repo_hidden"])
	}
	if author["head_repo"] != f.forker+"/"+f.forkRepo {
		t.Errorf("the fork's owner: head_repo = %v, want %s/%s", author["head_repo"], f.forker, f.forkRepo)
	}
}
