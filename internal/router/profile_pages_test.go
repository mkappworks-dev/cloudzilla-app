package router_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type profileEnv struct {
	h                    http.Handler
	svc                  *service.Services
	db                   *sql.DB
	owner, viewer, other metaUser
	suffix               string
}

func newProfileEnv(t *testing.T) profileEnv {
	t.Helper()
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	sfx := testutil.UniqueSuffix(t)
	user := func(tag string) metaUser {
		id := testutil.SeedUser(t, db, tag+sfx)
		name := "testuser_" + tag + sfx
		return metaUser{id: id, name: name, token: makeJWT(t, id, name)}
	}
	return profileEnv{h: h, svc: svc, db: db, owner: user("po"), viewer: user("pv"), other: user("px"), suffix: sfx}
}

func (e profileEnv) get(t *testing.T, target, token string) *httptest.ResponseRecorder {
	t.Helper()
	return serve(e.h, browserRequest(http.MethodGet, target, token, nil))
}

type repoSpec struct {
	name, description, language string
	private, fork, template     bool
}

func (e profileEnv) repo(t *testing.T, ownerID int64, ownerName string, s repoSpec) int64 {
	t.Helper()
	var lang any
	if s.language != "" {
		lang = s.language
	}
	var id int64
	if err := e.db.QueryRow(
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, is_fork, is_template, primary_language)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		ownerID, ownerName, s.name, s.description, s.private, s.fork, s.template, lang).Scan(&id); err != nil {
		t.Fatalf("insert repo %s: %v", s.name, err)
	}
	t.Cleanup(func() { testutil.Exec(t, e.db, `DELETE FROM repositories WHERE id = $1`, id) })
	return id
}

func link(owner, repo string) string { return `href="/` + owner + `/` + repo + `"` }

func TestProfilePages_UnknownOwnerIsNotFound(t *testing.T) {
	e := newProfileEnv(t)
	wantStatus(t, e.get(t, "/nobody_"+e.suffix, ""), http.StatusNotFound)
}

func TestProfilePages_RepositoriesTabFilters(t *testing.T) {
	e := newProfileEnv(t)
	o := e.owner.name
	s := e.suffix
	e.repo(t, e.owner.id, o, repoSpec{name: "alpha-" + s, description: "the first", language: "Go"})
	e.repo(t, e.owner.id, o, repoSpec{name: "beta-" + s, description: "Needle inside", language: "Rust"})
	e.repo(t, e.owner.id, o, repoSpec{name: "forked-" + s, fork: true, language: "Go"})
	e.repo(t, e.owner.id, o, repoSpec{name: "tmpl-" + s, template: true})
	e.repo(t, e.owner.id, o, repoSpec{name: "secret-" + s, private: true, language: "Go"})

	cases := []struct {
		name, query string
		token       string
		want, not   []string
	}{
		{"anonymous sees only public", "", "", []string{"alpha-", "beta-", "forked-", "tmpl-"}, []string{"secret-"}},
		{"owner sees private too", "", e.owner.token, []string{"alpha-", "secret-"}, nil},
		{"unrelated user sees only public", "", e.viewer.token, []string{"alpha-"}, []string{"secret-"}},
		{"name search", "&q=ALPHA", "", []string{"alpha-"}, []string{"beta-", "forked-"}},
		{"description search", "&q=needle", "", []string{"beta-"}, []string{"alpha-"}},
		{"sources exclude forks and templates", "&type=sources", "", []string{"alpha-", "beta-"}, []string{"forked-", "tmpl-"}},
		{"forks", "&type=forks", "", []string{"forked-"}, []string{"alpha-", "tmpl-"}},
		{"templates", "&type=templates", "", []string{"tmpl-"}, []string{"alpha-", "forked-"}},
		{"language", "&language=Rust", "", []string{"beta-"}, []string{"alpha-", "forked-"}},
		{"private only", "&status=private", e.owner.token, []string{"secret-"}, []string{"alpha-"}},
		{"public only", "&status=public", e.owner.token, []string{"alpha-"}, []string{"secret-"}},
		{"private filter cannot reveal to a stranger", "&status=private", e.viewer.token, nil, []string{"secret-", "alpha-"}},
	}
	for _, c := range cases {
		rr := e.get(t, "/"+o+"?tab=repositories"+c.query, c.token)
		if rr.Code != http.StatusOK {
			t.Errorf("%s: status %d", c.name, rr.Code)
			continue
		}
		body := rr.Body.String()
		for _, w := range c.want {
			if !strings.Contains(body, link(o, w+s)) {
				t.Errorf("%s: missing %s", c.name, w)
			}
		}
		for _, n := range c.not {
			if strings.Contains(body, link(o, n+s)) {
				t.Errorf("%s: unexpectedly lists %s", c.name, n)
			}
		}
	}
}

func TestProfilePages_RepositoriesTabPaginates(t *testing.T) {
	e := newProfileEnv(t)
	o := e.owner.name
	for i := 0; i < 23; i++ {
		e.repo(t, e.owner.id, o, repoSpec{name: fmt.Sprintf("r%02d-%s", i, e.suffix)})
	}
	count := func(page string) int {
		body := e.get(t, "/"+o+"?tab=repositories"+page, "").Body.String()
		return strings.Count(body, `href="/`+o+`/r`)
	}
	p1, p2 := count(""), count("&page=2")
	if p2 >= p1 || p2 < 3 {
		t.Errorf("page 1 lists %d repos and page 2 lists %d; want 20 then the remaining 3 (counts include repeated links)", p1, p2)
	}
	// An out-of-range page clamps to the last page instead of rendering nothing.
	if beyond := count("&page=99"); beyond != p2 {
		t.Errorf("page 99 lists %d links, want the last page's %d", beyond, p2)
	}
	wantStatus(t, e.get(t, "/"+o+"?tab=repositories&page=abc", ""), http.StatusOK)
	wantStatus(t, e.get(t, "/"+o+"?tab=repositories&page=-4", ""), http.StatusOK)
}

func TestProfilePages_StarsTab(t *testing.T) {
	e := newProfileEnv(t)
	o := e.owner.name
	starred := e.repo(t, e.other.id, e.other.name, repoSpec{name: "liked-" + e.suffix})
	hidden := e.repo(t, e.other.id, e.other.name, repoSpec{name: "hiddenstar-" + e.suffix, private: true})
	testutil.Exec(t, e.db, `INSERT INTO stars (user_id, repo_id) VALUES ($1, $2), ($1, $3)`, e.owner.id, starred, hidden)

	body := e.get(t, "/"+o+"?tab=stars", e.viewer.token).Body.String()
	if !strings.Contains(body, link(e.other.name, "liked-"+e.suffix)) {
		t.Error("the starred public repo is missing")
	}
	if strings.Contains(body, "hiddenstar-") {
		t.Error("a star on a private repo leaked to a viewer without access")
	}
	wantStatus(t, e.get(t, "/"+o+"?tab=stars&page=7", ""), http.StatusOK)
}

func TestProfilePages_GistsTabShowsPrivateGistsOnlyToTheOwner(t *testing.T) {
	e := newProfileEnv(t)
	o := e.owner.name
	ctx := context.Background()
	pub, err := e.svc.Gist.Create(ctx, e.owner.id, o, "public one", true, []model.GistFile{{Filename: "main.go", Content: "package main"}})
	if err != nil {
		t.Fatal(err)
	}
	priv, err := e.svc.Gist.Create(ctx, e.owner.id, o, "private one", false, []model.GistFile{{Filename: "notes.md", Content: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testutil.Exec(t, e.db, `DELETE FROM gists WHERE owner_id = $1`, e.owner.id) })

	for _, c := range []struct {
		name, token string
		wantPrivate bool
	}{{"anonymous", "", false}, {"other user", e.viewer.token, false}, {"owner", e.owner.token, true}} {
		body := e.get(t, "/"+o+"?tab=gists", c.token).Body.String()
		if !strings.Contains(body, pub.ID) {
			t.Errorf("%s: public gist missing", c.name)
		}
		if got := strings.Contains(body, priv.ID); got != c.wantPrivate {
			t.Errorf("%s: private gist listed = %v, want %v", c.name, got, c.wantPrivate)
		}
	}
	wantStatus(t, e.get(t, "/"+o+"?tab=gists&page=9", e.owner.token), http.StatusOK)
	wantStatus(t, e.get(t, "/"+o+"?tab=bogus", ""), http.StatusOK)
}

func TestProfilePages_OrgProfileHidesPrivateReposAndGatesManagement(t *testing.T) {
	e := newProfileEnv(t)
	ctx := context.Background()
	org, err := e.svc.Org.Create(ctx, e.owner.id, "testorg_"+e.suffix, "", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	testutil.DeleteOrgOnCleanup(t, e.db, org.ID)
	if err := e.svc.Org.AddMember(ctx, org.ID, e.owner.id, e.viewer.id, model.OrgRoleMember); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	pubRepo, err := e.svc.Org.CreateRepo(ctx, org.ID, e.owner.id, "open-"+e.suffix, "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	privRepo, err := e.svc.Org.CreateRepo(ctx, org.ID, e.owner.id, "closed-"+e.suffix, "", true, service.RepoInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		testutil.Exec(t, e.db, `DELETE FROM repositories WHERE id = ANY($1)`, []int64{pubRepo.ID, privRepo.ID})
	})

	path := "/" + org.Name
	anon := e.get(t, path+"?tab=repositories", "")
	wantStatus(t, anon, http.StatusOK)
	bodyHas(t, anon, pubRepo.Name)
	if strings.Contains(anon.Body.String(), privRepo.Name) {
		t.Error("anonymous visitor sees a private org repo")
	}
	// Org membership alone grants nothing on a private repo; a collaborator role does.
	if strings.Contains(e.get(t, path+"?tab=repositories", e.viewer.token).Body.String(), privRepo.Name) {
		t.Error("a plain org member sees a private repo they have no role on")
	}
	testutil.Exec(t, e.db, `INSERT INTO permissions (repo_id, user_id, role) VALUES ($1, $2, 'reader')`, privRepo.ID, e.viewer.id)
	bodyHas(t, e.get(t, path+"?tab=repositories", e.viewer.token), privRepo.Name)
	bodyHas(t, e.get(t, path+"?tab=repositories", e.owner.token), privRepo.Name)
	stranger := e.get(t, path+"?tab=repositories", e.other.token)
	if strings.Contains(stranger.Body.String(), privRepo.Name) {
		t.Error("a non-member sees a private org repo")
	}
	wantStatus(t, e.get(t, path+"?tab=people", e.viewer.token), http.StatusOK)
	wantStatus(t, e.get(t, path, ""), http.StatusOK)

	settings := "/orgs/" + org.Name + "/settings"
	wantStatus(t, e.get(t, settings, e.owner.token), http.StatusOK)
	wantStatus(t, e.get(t, settings, e.viewer.token), http.StatusForbidden)
	wantStatus(t, e.get(t, settings, e.other.token), http.StatusForbidden)
	wantStatus(t, e.get(t, "/orgs/missing-"+e.suffix+"/settings", e.owner.token), http.StatusNotFound)
	if rr := e.get(t, settings, ""); rr.Code == http.StatusOK {
		t.Errorf("anonymous org settings = %d", rr.Code)
	}

	list := e.get(t, "/organizations", e.viewer.token)
	wantStatus(t, list, http.StatusOK)
	bodyHas(t, list, org.Name)
	if strings.Contains(e.get(t, "/organizations", e.other.token).Body.String(), org.Name) {
		t.Error("/organizations lists an org the user does not belong to")
	}
	if rr := e.get(t, "/organizations", ""); rr.Code == http.StatusOK {
		t.Errorf("anonymous /organizations = %d", rr.Code)
	}
}
