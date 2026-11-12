package handler_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
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

type orgHookEnv struct {
	api                         http.Handler
	db                          *sql.DB
	orgName                     string
	orgID, repoID               int64
	ownerTok, memberTok, adminT string
	password                    string
}

func newOrgHookEnv(t *testing.T) orgHookEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	const password = "correct horse battery"
	ownerID, _ := testutil.SeedUserWithPassword(t, db, "o"+suffix, password)
	memberID := testutil.SeedUser(t, db, "m"+suffix)
	repoAdminID := testutil.SeedUser(t, db, "a"+suffix)

	e := orgHookEnv{db: db, orgName: "hooks_" + suffix, password: password}
	if err := db.QueryRow(`INSERT INTO organizations (name) VALUES ($1) RETURNING id`, e.orgName).Scan(&e.orgID); err != nil {
		t.Fatal(err)
	}
	testutil.DeleteOrgOnCleanup(t, db, e.orgID)
	testutil.Exec(t, db, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner'), ($1, $3, 'member')`, e.orgID, ownerID, memberID)
	if err := db.QueryRow(`INSERT INTO repositories (owner_name, org_id, name) VALUES ($1, $2, 'app') RETURNING id`, e.orgName, e.orgID).Scan(&e.repoID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM repositories WHERE id = $1`, e.repoID) })
	testutil.Exec(t, db, `INSERT INTO permissions (repo_id, user_id, role) VALUES ($1, $2, 'admin')`, e.repoID, repoAdminID)

	cfg := &config.Config{
		Auth: config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName},
		Git:  config.GitConfig{ReposRoot: t.TempDir()},
	}
	h := handler.New(service.New(store.New(db), cfg), cfg)
	r := chi.NewRouter()
	r.Route("/api/orgs/{org}/hooks", func(r chi.Router) {
		r.Get("/", h.ListOrgWebhooks)
		r.Post("/", h.CreateOrgWebhook)
		r.Delete("/{id}", h.DeleteOrgWebhook)
		r.Get("/{id}/deliveries", h.ListOrgWebhookDeliveries)
	})
	r.Get("/api/repos/{owner}/{repo}/hooks/", h.ListWebhooks)
	unauthorized := func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) }
	e.api = middleware.Auth(testJWTSecret, testCookieName, nil, nil, unauthorized)(r)
	e.ownerTok = makeIssueJWT(t, ownerID, "testpw_o"+suffix)
	e.memberTok = makeIssueJWT(t, memberID, "testuser_m"+suffix)
	e.adminT = makeIssueJWT(t, repoAdminID, "testuser_a"+suffix)
	return e
}

func (e orgHookEnv) do(method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	e.api.ServeHTTP(rr, req)
	return rr
}

func (e orgHookEnv) hooksPath() string { return "/api/orgs/" + e.orgName + "/hooks/" }

func (e orgHookEnv) seedHook(t *testing.T) int64 {
	t.Helper()
	var id int64
	if err := e.db.QueryRow(`INSERT INTO webhooks (org_id, url) VALUES ($1, 'https://example.test/h') RETURNING id`, e.orgID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestOrgWebhooks_OnlyOwnersManage(t *testing.T) {
	e := newOrgHookEnv(t)
	id := strconv.FormatInt(e.seedHook(t), 10)
	create := `{"url":"https://example.test/h","password":"` + e.password + `"}`

	for name, tok := range map[string]string{"member": e.memberTok, "repository admin": e.adminT} {
		for _, c := range []struct{ method, path, body string }{
			{http.MethodGet, e.hooksPath(), ""},
			{http.MethodPost, e.hooksPath(), create},
			{http.MethodDelete, e.hooksPath() + id, ""},
			{http.MethodGet, e.hooksPath() + id + "/deliveries", ""},
		} {
			if rr := e.do(c.method, c.path, tok, c.body); rr.Code != http.StatusForbidden {
				t.Errorf("%s %s %s = %d; want 403", name, c.method, c.path, rr.Code)
			}
		}
	}
	if rr := e.do(http.MethodGet, e.hooksPath(), "", ""); rr.Code != http.StatusUnauthorized {
		t.Errorf("anonymous list = %d; want 401", rr.Code)
	}
}

func TestOrgWebhooks_OwnerCreatesListsAndDeletes(t *testing.T) {
	e := newOrgHookEnv(t)
	rr := e.do(http.MethodPost, e.hooksPath(), e.ownerTok, `{"url":"https://example.test/h","secret":"s","password":"`+e.password+`"}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", rr.Code, rr.Body)
	}
	if strings.Contains(rr.Body.String(), `"s"`) || !strings.Contains(rr.Body.String(), `"org_id"`) {
		t.Errorf("create body = %s; want org_id and no secret", rr.Body)
	}

	rr = e.do(http.MethodGet, e.hooksPath(), e.ownerTok, "")
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "https://example.test/h") {
		t.Fatalf("list = %d: %s", rr.Code, rr.Body)
	}
	// The repository's own endpoint doesn't list the organization's hook.
	rr = e.do(http.MethodGet, "/api/repos/"+e.orgName+"/app/hooks/", e.ownerTok, "")
	if rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != "[]" {
		t.Errorf("repo hooks = %d: %s; want []", rr.Code, rr.Body)
	}

	var id int64
	if err := e.db.QueryRow(`SELECT id FROM webhooks WHERE org_id = $1`, e.orgID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if rr := e.do(http.MethodDelete, e.hooksPath()+strconv.FormatInt(id, 10), e.ownerTok, ""); rr.Code != http.StatusNoContent {
		t.Errorf("delete = %d: %s", rr.Code, rr.Body)
	}
}

func TestOrgWebhooks_CreateNeedsConfirmationAndAllowedURL(t *testing.T) {
	e := newOrgHookEnv(t)

	// The default config refuses a loopback payload URL.
	rr := e.do(http.MethodPost, e.hooksPath(), e.ownerTok, `{"url":"http://127.0.0.1:9/hook","password":"`+e.password+`"}`)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "private network") {
		t.Errorf("refused URL = %d: %s; want 400 with the reason", rr.Code, rr.Body)
	}
	if rr := e.do(http.MethodPost, e.hooksPath(), e.ownerTok, `{"url":"ftp://example.test/h","password":"`+e.password+`"}`); rr.Code != http.StatusBadRequest {
		t.Errorf("ftp URL = %d; want 400", rr.Code)
	}
	if rr := e.do(http.MethodPost, e.hooksPath(), e.ownerTok, `{"url":"https://example.test/h","password":"wrong"}`); rr.Code == http.StatusCreated {
		t.Errorf("wrong password created the webhook: %s", rr.Body)
	}
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM webhooks WHERE org_id = $1`, e.orgID).Scan(&n); err != nil || n != 0 {
		t.Errorf("stored %d webhooks, %v; want none", n, err)
	}
}
