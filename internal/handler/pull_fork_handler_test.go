package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestCreatePull_FromForkHead_201(t *testing.T) {
	db := testutil.OpenTestDB(t)
	root := t.TempDir()
	suffix := testutil.UniqueSuffix(t)
	upID := testutil.SeedUser(t, db, suffix)
	upOwner, upRepo := "testuser_"+suffix, "testrepo_"+suffix
	testutil.SeedRepo(t, db, upID, upOwner, suffix)
	up := testutil.InitBareRepo(t, filepath.Join(root, upOwner, upRepo+".git"))
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
	router := pullRouterWithAuth(handler.New(svc, cfg))

	forkerID := testutil.SeedUser(t, db, suffix+"_f")
	forker := "testuser_" + suffix + "_f"
	fork, err := repoSvc.Fork(context.Background(), upOwner, upRepo, forkerID, forker, service.ForkOptions{})
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	forkGit, err := gogit.PlainOpen(filepath.Join(root, forker, fork.Name+".git"))
	if err != nil {
		t.Fatal(err)
	}
	forkSHA := testutil.WriteCommit(t, forkGit.Storer, "fork change", main)
	if err := forkGit.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName("fix-x"), forkSHA)); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{
		"title": "Fix x", "head_branch": "fix-x", "base_branch": "main", "head_repo": forker + "/" + fork.Name,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/"+upOwner+"/"+upRepo+"/pulls", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeIssueJWT(t, forkerID, forker))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["head_repo"] != forker+"/"+fork.Name {
		t.Errorf("head_repo = %v, want %s/%s", got["head_repo"], forker, fork.Name)
	}
	if got["head_sha"] != forkSHA.String() {
		t.Errorf("head_sha = %v, want %s", got["head_sha"], forkSHA)
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
