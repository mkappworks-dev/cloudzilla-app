package handler_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestCreateOrg_InvalidName_400WithRule(t *testing.T) {
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

	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", rr.Code, rr.Body)
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
