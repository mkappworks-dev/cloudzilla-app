package router_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// forkEnv has owner's public owner/src, with a README, for user to fork.
type forkEnv struct {
	h               http.Handler
	svc             *service.Services
	db              *sql.DB
	owner, user     string
	ownerID, userID int64
}

func newForkEnv(t *testing.T, smtp config.SMTPConfig) forkEnv {
	t.Helper()
	h, svc, db := newVerificationRouter(t, smtp)
	ownerSuffix, userSuffix := testutil.UniqueSuffix(t), testutil.UniqueSuffix(t)
	e := forkEnv{
		h: h, svc: svc, db: db,
		ownerID: testutil.SeedUser(t, db, ownerSuffix), owner: "testuser_" + ownerSuffix,
		userID: testutil.SeedUser(t, db, userSuffix), user: "testuser_" + userSuffix,
	}
	if _, err := svc.Repo.Create(context.Background(), e.ownerID, e.owner, "src", "the source", false, service.RepoInitOptions{AddREADME: true}); err != nil {
		t.Fatalf("create source: %v", err)
	}
	return e
}

func (e forkEnv) org(t *testing.T, ownerID int64) *model.Organization {
	t.Helper()
	org, err := e.svc.Org.Create(context.Background(), ownerID, "testorg_"+testutil.UniqueSuffix(t), "", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	testutil.DeleteOrgOnCleanup(t, e.db, org.ID)
	return org
}

func (e forkEnv) forkURL() string { return "/api/repos/" + e.owner + "/src/fork" }

func jsonPost(target, session, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if session != "" {
		req.AddCookie(&http.Cookie{Name: "cz_token", Value: session})
	}
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: testCSRF})
	req.Header.Set("X-CSRF-Token", testCSRF)
	return req
}

func TestForkAPI_JSONBodyPicksOwnerNameAndDescription(t *testing.T) {
	e := newForkEnv(t, config.SMTPConfig{})
	org := e.org(t, e.userID)

	rr := serve(e.h, jsonPost(e.forkURL(), makeJWT(t, e.userID, e.user),
		`{"owner":"`+org.Name+`","name":"copy","description":"","default_branch_only":true}`))

	if rr.Code != http.StatusCreated {
		t.Fatalf("got %d %s, want 201", rr.Code, rr.Body)
	}
	var got struct{ Owner, Name, URL string }
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode %s: %v", rr.Body, err)
	}
	if got.Owner != org.Name || got.Name != "copy" || got.URL != "/"+org.Name+"/copy" {
		t.Errorf("response = %+v", got)
	}
	fork, err := e.svc.Repo.Get(context.Background(), org.Name, "copy")
	if err != nil {
		t.Fatalf("fork not created: %v", err)
	}
	if fork.Description != "" || fork.OrgID != org.ID {
		t.Errorf("fork = description %q org %d, want an empty description in org %d", fork.Description, fork.OrgID, org.ID)
	}
}

func TestForkAPI_AFormPostForksIntoTheCallersAccount(t *testing.T) {
	e := newForkEnv(t, config.SMTPConfig{})

	rr := serve(e.h, browserRequest(http.MethodPost, e.forkURL(), makeJWT(t, e.userID, e.user), url.Values{}))

	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/"+e.user+"/src" {
		t.Fatalf("got %d to %q, want 303 to /%s/src", rr.Code, rr.Header().Get("Location"), e.user)
	}
	fork, err := e.svc.Repo.Get(context.Background(), e.user, "src")
	if err != nil {
		t.Fatalf("fork not created: %v", err)
	}
	if fork.Description != "the source" {
		t.Errorf("description = %q, want the source's", fork.Description)
	}
}

func TestForkAPI_AnEmptyJSONBodyForksWithTheDefaults(t *testing.T) {
	e := newForkEnv(t, config.SMTPConfig{})

	rr := serve(e.h, jsonPost(e.forkURL(), makeJWT(t, e.userID, e.user), ""))

	if rr.Code != http.StatusCreated {
		t.Fatalf("got %d %s, want 201", rr.Code, rr.Body)
	}
	var got struct{ URL string }
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode %s: %v", rr.Body, err)
	}
	if got.URL != "/"+e.user+"/src" {
		t.Errorf("url = %q, want /%s/src", got.URL, e.user)
	}
	if _, err := e.svc.Repo.Get(context.Background(), e.user, "src"); err != nil {
		t.Errorf("fork not created in the caller's account: %v", err)
	}
}

func TestForkAPI_Refusals(t *testing.T) {
	e := newForkEnv(t, config.SMTPConfig{})
	if _, err := e.svc.Repo.Create(context.Background(), e.userID, e.user, "taken", "", false, service.RepoInitOptions{}); err != nil {
		t.Fatalf("create clash: %v", err)
	}
	stranger := e.org(t, e.ownerID)
	asOwner, asUser := makeJWT(t, e.ownerID, e.owner), makeJWT(t, e.userID, e.user)

	cases := []struct {
		name, session, body string
		want                int
		wantBody            string
	}{
		{"malformed JSON", asUser, `{"owner":`, http.StatusBadRequest, "invalid request body"},
		{"the source's own account", asOwner, `{}`, http.StatusUnprocessableEntity, "can't be forked into the account or organization that owns it"},
		{"an org the caller doesn't own", asUser, `{"owner":"` + stranger.Name + `"}`, http.StatusForbidden, "you can fork only into your account or an organization you own"},
		{"a taken name", asUser, `{"name":"taken"}`, http.StatusUnprocessableEntity, "already exists"},
		{"a path as the name", asUser, `{"name":"../x"}`, http.StatusUnprocessableEntity, ""},
	}
	for _, c := range cases {
		rr := serve(e.h, jsonPost(e.forkURL(), c.session, c.body))
		if rr.Code != c.want || !strings.Contains(rr.Body.String(), c.wantBody) {
			t.Errorf("%s: got %d %s, want %d %q", c.name, rr.Code, rr.Body, c.want, c.wantBody)
		}
	}
	if n := countRows(t, e.db, `SELECT COUNT(*) FROM repositories WHERE is_fork AND (owner_name = $1 OR owner_name = $2 OR owner_name = $3)`, e.owner, e.user, stranger.Name); n != 0 {
		t.Errorf("%d forks created by refused requests", n)
	}
}

// The middleware checks a target-limited token against the path, which names
// the source; the org a fork lands in comes from the body.
func TestForkAPI_TargetLimitedTokenStaysInItsOrgs(t *testing.T) {
	smtp, _ := testutil.FakeSMTP(t)
	e := newForkEnv(t, smtp)
	ctx := context.Background()
	src, err := e.svc.Repo.Get(ctx, e.owner, "src")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Repo.AddCollaborator(ctx, src, e.ownerID, e.user, string(model.RoleAdmin)); err != nil {
		t.Fatalf("make user an admin of the source: %v", err)
	}
	named, unnamed := e.org(t, e.userID), e.org(t, e.userID)
	signer, signingKey := testutil.NewSigningKey(t)
	soon := time.Now().Add(24 * time.Hour)
	token, _, err := e.svc.AccessToken.Create(ctx, e.userID, service.NewToken{
		Name:       "fork",
		Scopes:     []string{model.ScopeRepoAdmin, model.ScopeRepoWrite},
		ExpiresAt:  &soon,
		SigningKey: signingKey,
		Targets:    []string{e.owner + "/src", named.Name},
	})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	fork := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, e.forkURL(), strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		testutil.SignHTTPRequest(t, signer, req)
		return serve(e.h, req)
	}

	if rr := fork(`{"owner":"` + unnamed.Name + `"}`); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "isn't allowed for that repository or organization") {
		t.Errorf("an org the token doesn't name: got %d %s, want 403", rr.Code, rr.Body)
	}
	if n := countRows(t, e.db, `SELECT COUNT(*) FROM repositories WHERE owner_name = $1`, unnamed.Name); n != 0 {
		t.Errorf("%d repos created in an org the token doesn't name", n)
	}
	if rr := fork(`{"owner":"` + named.Name + `"}`); rr.Code != http.StatusCreated {
		t.Errorf("an org the token names: got %d %s, want 201", rr.Code, rr.Body)
	}
	if rr := fork(`{}`); rr.Code != http.StatusCreated {
		t.Errorf("the token user's own account: got %d %s, want 201", rr.Code, rr.Body)
	}
}
