package handler_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
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

func TestCreateOrg_InvalidName_422WithRule(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM organizations WHERE name = '..'`) })
	h := newAuthHandler(db)
	h.Services.Org = service.NewOrgService(store.NewOrgStore(db), store.NewRepoStore(db), store.NewUserStore(db), config.GitConfig{})
	authMW := middleware.Auth(testJWTSecret, testCookieName, nil, nil, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) })

	req := httptest.NewRequest(http.MethodPost, "/api/orgs", strings.NewReader(`{"name":".."}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeIssueJWT(t, userID, "testuser_"+suffix))
	rr := httptest.NewRecorder()
	authMW(http.HandlerFunc(h.CreateOrg)).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("want 422, got %d: %s", rr.Code, rr.Body)
	}
	if !strings.Contains(rr.Body.String(), "Organization names can use letters, numbers, - and _") {
		t.Errorf("want the name rule; body: %s", rr.Body)
	}
}

func TestCreateOrg_NameOfUserInOtherCase_422Taken(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	name := strings.ToUpper("testuser_" + suffix)
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM organizations WHERE name = $1`, name) })
	h := newAuthHandler(db)
	h.Services.Org = service.NewOrgService(store.NewOrgStore(db), store.NewRepoStore(db), store.NewUserStore(db), config.GitConfig{})
	authMW := middleware.Auth(testJWTSecret, testCookieName, nil, nil, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) })

	req := httptest.NewRequest(http.MethodPost, "/api/orgs", strings.NewReader(`{"name":"`+name+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeIssueJWT(t, userID, "testuser_"+suffix))
	rr := httptest.NewRecorder()
	authMW(http.HandlerFunc(h.CreateOrg)).ServeHTTP(rr, req)

	if want := `{"error":"That name is already taken"}` + "\n"; rr.Code != http.StatusUnprocessableEntity || rr.Body.String() != want {
		t.Errorf("want 422 %s, got %d %s", want, rr.Code, rr.Body)
	}
}

func seedOrgOwnedBy(t *testing.T, db *sql.DB, name string, ownerID int64) {
	t.Helper()
	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations (name) VALUES ($1) RETURNING id`, name).Scan(&orgID); err != nil {
		t.Fatalf("seed org %q: %v", name, err)
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM organizations WHERE id = $1`, orgID) })
	testutil.Exec(t, db, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`, orgID, ownerID)
}

func postOrgRepo(t *testing.T, api http.Handler, token, org, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/orgs/"+org+"/repos", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	api.ServeHTTP(rr, req)
	return rr
}

func TestCreateOrgRepo_InvalidName_422WithRule(t *testing.T) {
	db := testutil.OpenTestDB(t)
	api := newAPIRouter(t, db)
	user := seedSignedInUser(t, db)
	org := "org_" + testutil.UniqueSuffix(t)
	seedOrgOwnedBy(t, db, org, user.id)
	logs := captureLogs(t)

	rr := postOrgRepo(t, api, user.token, org, `{"name":"a b"}`)

	if rr.Code != http.StatusUnprocessableEntity || !strings.Contains(rr.Body.String(), "Repository names can use letters") {
		t.Errorf("want 422 with the repository name rule, got %d %s", rr.Code, rr.Body)
	}
	if strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Errorf("a client's invalid name isn't a server error; logs:\n%s", logs)
	}
}

func TestCreateOrgRepo_LegacyUnsafeOrgName_422AndWarns(t *testing.T) {
	db := testutil.OpenTestDB(t)
	api := newAPIRouter(t, db)
	user := seedSignedInUser(t, db)
	org := "*_" + testutil.UniqueSuffix(t)
	seedOrgOwnedBy(t, db, org, user.id)
	logs := captureLogs(t)

	rr := postOrgRepo(t, api, user.token, org, `{"name":"copy"}`)

	if rr.Code != http.StatusUnprocessableEntity || !strings.Contains(rr.Body.String(), "can't be created") {
		t.Errorf("want 422 with the unsafe path message, got %d %s", rr.Code, rr.Body)
	}
	if level := loggedLevel(t, logs, "create org repo: unsafe repository path"); level != "WARN" {
		t.Errorf("want a WARN log, got %s", level)
	}
}

func newOrgTestRouter(t *testing.T, db *sql.DB) (http.Handler, *service.Services) {
	t.Helper()
	cfg := &config.Config{
		Auth: config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName},
		Git:  config.GitConfig{ReposRoot: t.TempDir()},
	}
	svc := service.New(store.New(db), cfg)
	h := handler.New(svc, cfg)
	r := chi.NewRouter()
	r.Post("/api/orgs/", h.CreateOrg)
	r.Post("/organizations/new", h.CreateOrganization)
	r.Post("/api/orgs/{org}/transfer", h.TransferOrg)
	r.Get("/repos/new", h.PageNewRepo)
	r.Post("/api/orgs/{org}/repos", h.CreateOrgRepo)
	unauthorized := func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) }
	return middleware.Auth(testJWTSecret, testCookieName, nil, nil, unauthorized)(r), svc
}

func TestCreateOrg_RejectsInvalidName(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	router, svc := newOrgTestRouter(t, db)

	for _, name := range append([]string{"a b", "../x"}, testutil.HostileNames...) {
		body, _ := json.Marshal(map[string]string{"name": name})
		req := httptest.NewRequest(http.MethodPost, "/api/orgs/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+makeIssueJWT(t, userID, "testuser_"+suffix))
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnprocessableEntity {
			t.Errorf("name %q: want 422, got %d: %s", name, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "Organization names can use letters") {
			t.Errorf("name %q: body %s does not give the name rule", name, rr.Body.String())
		}
		if _, err := svc.Org.Get(context.Background(), name); err == nil {
			t.Errorf("name %q: org was stored", name)
			testutil.Exec(t, db, `DELETE FROM organizations WHERE name = $1`, name)
		}
	}
}

func TestCreateOrganizationPage_RejectsInvalidNameOnce(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	router, _ := newOrgTestRouter(t, db)

	form := url.Values{"name": {"',a:alert(1),b:'"}, "accept_tos": {"on"}}
	req := httptest.NewRequest(http.MethodPost, "/organizations/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+makeIssueJWT(t, userID, "testuser_"+suffix))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d", rr.Code)
	}
	if n := strings.Count(rr.Body.String(), "Organization names can use letters"); n != 1 {
		t.Errorf("invalid-name message shown %d times, want 1", n)
	}
}

// The previous owner is demoted to member by the transfer, so sending them
// back to the owner-only settings page would land on a 403.
func TestTransferOrg_RedirectsToOrgPage(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	newOwnerSuffix := suffix + "_new"
	testutil.SeedUser(t, db, newOwnerSuffix)
	router, svc := newOrgTestRouter(t, db)

	org, err := svc.Org.Create(context.Background(), ownerID, "testorg_"+suffix, "", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	form := url.Values{"new_owner": {"testuser_" + newOwnerSuffix}, "confirm_name": {org.Name}}
	req := httptest.NewRequest(http.MethodPost, "/api/orgs/"+org.Name+"/transfer", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+makeIssueJWT(t, ownerID, "testuser_"+suffix))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d: %s", rr.Code, rr.Body.String())
	}
	if loc := rr.Header().Get("Location"); loc != "/"+org.Name {
		t.Errorf("Location = %q, want %q", loc, "/"+org.Name)
	}
}

var checkedPrivateRadio = regexp.MustCompile(`<input type="radio" name="visibility" value="private" checked`)

func TestPageNewRepo_OwnerOrgDefaultVisibility(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	router, svc := newOrgTestRouter(t, db)
	ctx := context.Background()

	org, err := svc.Org.Create(ctx, ownerID, "testorg_"+suffix, "", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	get := func(query string) string {
		req := httptest.NewRequest(http.MethodGet, "/repos/new"+query, nil)
		req.Header.Set("Authorization", "Bearer "+makeIssueJWT(t, ownerID, "testuser_"+suffix))
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET /repos/new%s: want 200, got %d", query, rr.Code)
		}
		return rr.Body.String()
	}

	if err := svc.Org.UpdateRepoDefaults(ctx, org.ID, ownerID, "private", "main"); err != nil {
		t.Fatalf("UpdateRepoDefaults: %v", err)
	}
	if !checkedPrivateRadio.MatchString(get("?owner=" + org.Name)) {
		t.Error("private-by-default org: Private radio not preselected")
	}
	if checkedPrivateRadio.MatchString(get("")) {
		t.Error("personal owner: Private radio preselected, want Public")
	}
	if !checkedPrivateRadio.MatchString(get("?owner=" + org.Name + "&visibility=private")) {
		t.Error("explicit ?visibility=private ignored")
	}

	if err := svc.Org.UpdateRepoDefaults(ctx, org.ID, ownerID, "public", "main"); err != nil {
		t.Fatalf("UpdateRepoDefaults: %v", err)
	}
	if checkedPrivateRadio.MatchString(get("?owner=" + org.Name)) {
		t.Error("public-by-default org: Private radio preselected")
	}
}

func TestCreateOrgRepo_OmittedPrivateUsesOrgDefault(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	router, svc := newOrgTestRouter(t, db)
	ctx := context.Background()

	org, err := svc.Org.Create(ctx, ownerID, "testorg_"+suffix, "", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	create := func(body string) *http.Response {
		req := httptest.NewRequest(http.MethodPost, "/api/orgs/"+org.Name+"/repos", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+makeIssueJWT(t, ownerID, "testuser_"+suffix))
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("POST %s: want 201, got %d: %s", body, rr.Code, rr.Body.String())
		}
		return rr.Result()
	}
	isPrivate := func(name string) bool {
		repo, err := svc.Repo.Get(ctx, org.Name, name)
		if err != nil {
			t.Fatalf("get %s: %v", name, err)
		}
		return repo.Private
	}

	if err := svc.Org.UpdateRepoDefaults(ctx, org.ID, ownerID, "private", "main"); err != nil {
		t.Fatalf("UpdateRepoDefaults: %v", err)
	}
	create(`{"name":"omitted"}`)
	if !isPrivate("omitted") {
		t.Error("private-by-default org: repo created without \"private\" is public")
	}
	create(`{"name":"explicit","private":false}`)
	if isPrivate("explicit") {
		t.Error("explicit \"private\": false ignored")
	}
}

// A directory left without a row must read as a taken name, not as a failure
// that leaves a row bound to it.
func TestCreateOrgRepo_LeftoverDir_422(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	router, svc := newOrgTestRouter(t, db)
	ctx := context.Background()

	org, err := svc.Org.Create(ctx, ownerID, "testorg_"+suffix, "", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM organizations WHERE id = $1`, org.ID) })
	post := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/orgs/"+org.Name+"/repos", strings.NewReader(`{"name":"left"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+makeIssueJWT(t, ownerID, "testuser_"+suffix))
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		return rr
	}

	if rr := post(); rr.Code != http.StatusCreated {
		t.Fatalf("first create: want 201, got %d: %s", rr.Code, rr.Body.String())
	}
	testutil.Exec(t, db, `DELETE FROM repositories WHERE owner_name = $1 AND name = 'left'`, org.Name)

	rr := post()
	if rr.Code != http.StatusUnprocessableEntity || !strings.Contains(rr.Body.String(), "already exists") {
		t.Errorf("create over leftover dir: want 422 naming the conflict, got %d: %s", rr.Code, rr.Body.String())
	}
	if _, err := svc.Repo.Get(ctx, org.Name, "left"); err == nil {
		t.Error("a row was bound to the leftover dir")
	}
}
