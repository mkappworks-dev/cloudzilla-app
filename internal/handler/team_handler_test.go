package handler_test

import (
	"database/sql"
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
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type teamHandlerEnv struct {
	api                           http.Handler
	db                            *sql.DB
	orgName                       string
	ownerTok, memberTok, outsider string
	memberName                    string
}

func newTeamHandlerEnv(t *testing.T) teamHandlerEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, "o"+suffix)
	memberID := testutil.SeedUser(t, db, "m"+suffix)
	outsiderID := testutil.SeedUser(t, db, "x"+suffix)
	e := teamHandlerEnv{db: db, orgName: "tm_" + suffix, memberName: "testuser_m" + suffix}
	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations (name) VALUES ($1) RETURNING id`, e.orgName).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	testutil.DeleteOrgOnCleanup(t, db, orgID)
	testutil.Exec(t, db, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner'), ($1, $3, 'member')`, orgID, ownerID, memberID)

	cfg := &config.Config{
		Auth: config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName},
		Git:  config.GitConfig{ReposRoot: t.TempDir()},
	}
	h := handler.New(service.New(store.New(db), cfg), cfg)
	r := chi.NewRouter()
	r.Get("/orgs/{org}/teams/{slug}", h.PageOrgTeam)
	r.Route("/api/orgs/{org}/teams", func(r chi.Router) {
		r.Get("/", h.ListTeams)
		r.Post("/", h.CreateTeam)
		r.Post("/{slug}", h.UpdateTeam)
		r.Delete("/{slug}", h.DeleteTeam)
		r.Get("/{slug}/members", h.ListTeamMembers)
		r.Post("/{slug}/members", h.AddTeamMember)
		r.Delete("/{slug}/members/{username}", h.RemoveTeamMember)
	})
	unauthorized := func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) }
	e.api = middleware.Auth(testJWTSecret, testCookieName, nil, nil, unauthorized)(r)
	e.ownerTok = makeIssueJWT(t, ownerID, "testuser_o"+suffix)
	e.memberTok = makeIssueJWT(t, memberID, e.memberName)
	e.outsider = makeIssueJWT(t, outsiderID, "testuser_x"+suffix)
	return e
}

func (e teamHandlerEnv) do(method, path, token string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	e.api.ServeHTTP(rr, req)
	return rr
}

func (e teamHandlerEnv) teamsPath() string { return "/api/orgs/" + e.orgName + "/teams/" }

func TestTeamHandlers_OwnerEditsMembersSeeOutsidersDont(t *testing.T) {
	e := newTeamHandlerEnv(t)

	if rr := e.do("POST", e.teamsPath(), e.memberTok, url.Values{"name": {"Core"}}); rr.Code != http.StatusForbidden {
		t.Errorf("member create = %d, want 403", rr.Code)
	}
	if rr := e.do("POST", e.teamsPath(), e.ownerTok, url.Values{"name": {"Core"}, "description": {"d"}}); rr.Code != http.StatusCreated {
		t.Fatalf("owner create = %d: %s", rr.Code, rr.Body)
	}
	if rr := e.do("POST", e.teamsPath(), e.ownerTok, url.Values{"name": {"core"}}); rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("duplicate create = %d, want 422", rr.Code)
	}
	members := e.teamsPath() + "core/members"
	if rr := e.do("POST", members, e.ownerTok, url.Values{"username": {"testuser_x" + strings.TrimPrefix(e.orgName, "tm_")}}); rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("adding a non-member = %d, want 422", rr.Code)
	}
	if rr := e.do("POST", members, e.ownerTok, url.Values{"username": {e.memberName}}); rr.Code != http.StatusCreated {
		t.Fatalf("owner adds member = %d: %s", rr.Code, rr.Body)
	}
	if rr := e.do("POST", members, e.memberTok, url.Values{"username": {e.memberName}}); rr.Code != http.StatusForbidden {
		t.Errorf("member adds member = %d, want 403", rr.Code)
	}

	for _, path := range []string{e.teamsPath(), members} {
		rr := e.do("GET", path, e.memberTok, nil)
		if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Core") && !strings.Contains(rr.Body.String(), e.memberName) {
			t.Errorf("member GET %s = %d: %s", path, rr.Code, rr.Body)
		}
		if rr := e.do("GET", path, e.outsider, nil); rr.Code != http.StatusNotFound {
			t.Errorf("outsider GET %s = %d, want 404", path, rr.Code)
		}
	}

	if rr := e.do("GET", "/orgs/"+e.orgName+"/teams/core", e.memberTok, nil); rr.Code != http.StatusOK ||
		!strings.Contains(rr.Body.String(), e.memberName) || strings.Contains(rr.Body.String(), "Delete team") {
		t.Errorf("member page = %d, want 200 with the roster and no owner controls", rr.Code)
	}
	if rr := e.do("GET", "/orgs/"+e.orgName+"/teams/core", e.ownerTok, nil); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Delete team") ||
		!strings.Contains(rr.Body.String(), ">d</textarea>") {
		t.Errorf("owner page = %d, want 200 with owner controls and the description prefilled", rr.Code)
	}
	if rr := e.do("GET", "/orgs/"+e.orgName+"/teams/core", e.outsider, nil); rr.Code != http.StatusNotFound {
		t.Errorf("outsider page = %d, want 404", rr.Code)
	}

	if rr := e.do("DELETE", e.teamsPath()+"core", e.memberTok, nil); rr.Code != http.StatusForbidden {
		t.Errorf("member delete = %d, want 403", rr.Code)
	}
	if rr := e.do("DELETE", members+"/"+e.memberName, e.ownerTok, nil); rr.Code != http.StatusNoContent {
		t.Errorf("owner removes member = %d", rr.Code)
	}
	if rr := e.do("DELETE", e.teamsPath()+"core", e.ownerTok, nil); rr.Code != http.StatusNoContent {
		t.Errorf("owner delete = %d", rr.Code)
	}
	if rr := e.do("GET", "/orgs/"+e.orgName+"/teams/core", e.ownerTok, nil); rr.Code != http.StatusNotFound {
		t.Errorf("deleted team page = %d, want 404", rr.Code)
	}
}
