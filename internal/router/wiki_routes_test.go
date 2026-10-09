package router_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

func (e r2Env) wikiAPI(slug string) string { return e.api("/wiki/" + slug) }

func (e r2Env) wikiPage(slug string) string { return "/" + e.owner.name + "/proj/wiki/" + slug }

func (e r2Env) saveWiki(t *testing.T, token, slug, content string) {
	t.Helper()
	rr := e.form(t, http.MethodPost, e.wikiAPI(slug), token, url.Values{"content": {content}}, false)
	wantStatus(t, rr, http.StatusSeeOther)
}

func (e r2Env) disableWiki(t *testing.T) {
	t.Helper()
	e.exec(t, `UPDATE repositories SET allow_wiki = false WHERE id = $1`)
}

func TestWikiRoutes_SaveRenderAndList(t *testing.T) {
	e := newR2Env(t)

	rr := e.form(t, http.MethodPost, e.wikiAPI("Home"), e.writer.token, url.Values{"content": {"# Welcome\n\nhello wiki"}, "message": {"first"}}, false)
	wantStatus(t, rr, http.StatusSeeOther)
	if loc := rr.Header().Get("Location"); loc != e.wikiPage("Home") {
		t.Errorf("Location = %q", loc)
	}
	e.saveWiki(t, e.writer.token, "Second-Page", "second body")

	rr = e.form(t, http.MethodGet, e.wikiPage("Home"), "", nil, false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "Welcome")
	bodyHas(t, rr, "Second-Page")

	content, found, err := e.svc.Code.WikiPageGet(e.owner.name, "proj", "Home")
	if err != nil || !found || !strings.Contains(content, "hello wiki") {
		t.Errorf("stored page = %q, %v, %v", content, found, err)
	}

	// A page that was never written still renders, as the "create it" prompt.
	wantStatus(t, e.form(t, http.MethodGet, e.wikiPage("Missing"), "", nil, false), http.StatusOK)

	rr = e.form(t, http.MethodGet, "/"+e.owner.name+"/proj/wiki", "", nil, false)
	wantStatus(t, rr, http.StatusFound)
	if loc := rr.Header().Get("Location"); loc != e.wikiPage("Home") {
		t.Errorf("wiki home redirect = %q", loc)
	}
}

func TestWikiRoutes_SlugsCannotEscapeTheWikiRepo(t *testing.T) {
	e := newR2Env(t)
	for _, slug := range []string{"a.b", "a%20b", "..%2F..%2Fconfig", "a%2Fb", strings.Repeat("a", 101), "page.md"} {
		rr := e.form(t, http.MethodPost, e.wikiAPI(slug), e.owner.token, url.Values{"content": {"x"}}, false)
		if rr.Code != http.StatusBadRequest && rr.Code != http.StatusNotFound {
			t.Errorf("POST slug %q = %d, want 400/404", slug, rr.Code)
		}
		wantStatus(t, e.form(t, http.MethodGet, e.wikiPage(slug), "", nil, false), rr.Code)
		rr = e.form(t, http.MethodDelete, e.wikiAPI(slug), e.owner.token, nil, false)
		if rr.Code != http.StatusBadRequest && rr.Code != http.StatusNotFound {
			t.Errorf("DELETE slug %q = %d, want 400/404", slug, rr.Code)
		}
	}
	wantStatus(t, e.form(t, http.MethodPost, e.wikiAPI("old"), e.owner.token, url.Values{"content": {"x"}, "new_slug": {"a/../b"}}, false), http.StatusBadRequest)
	wantStatus(t, e.form(t, http.MethodPost, e.wikiAPI("old"), e.owner.token, url.Values{"content": {"x"}, "new_slug": {"..%2Fx"}}, false), http.StatusBadRequest)
	if pages, _ := e.svc.Code.WikiPageList(e.owner.name, "proj"); len(pages) != 0 {
		t.Errorf("invalid slugs created pages: %v", pages)
	}
}

func TestWikiRoutes_WritePermissions(t *testing.T) {
	e := newR2Env(t)
	form := url.Values{"content": {"x"}}
	wantStatus(t, e.form(t, http.MethodPost, e.wikiAPI("P"), e.outsider.token, form, false), http.StatusForbidden)
	wantStatus(t, e.form(t, http.MethodPost, e.wikiAPI("P"), "", form, false), http.StatusUnauthorized)
	wantStatus(t, e.form(t, http.MethodPost, e.api("/wiki/order"), e.outsider.token, url.Values{"order": {"P"}}, false), http.StatusForbidden)
	e.saveWiki(t, e.writer.token, "P", "x")

	// Deleting is a manage action: a writer may edit pages but not remove them.
	wantStatus(t, e.form(t, http.MethodDelete, e.wikiAPI("P"), e.writer.token, nil, false), http.StatusForbidden)
	wantStatus(t, e.form(t, http.MethodDelete, e.wikiAPI("P"), e.outsider.token, nil, false), http.StatusForbidden)
	if _, found, _ := e.svc.Code.WikiPageGet(e.owner.name, "proj", "P"); !found {
		t.Fatal("refused delete removed the page")
	}
	wantStatus(t, e.form(t, http.MethodDelete, e.wikiAPI("P"), e.admin.token, nil, false), http.StatusSeeOther)
	if _, found, _ := e.svc.Code.WikiPageGet(e.owner.name, "proj", "P"); found {
		t.Error("page still there after admin delete")
	}
}

func TestWikiRoutes_DeleteHTMXAnswers200(t *testing.T) {
	e := newR2Env(t)
	e.saveWiki(t, e.owner.token, "Gone", "x")
	wantStatus(t, e.form(t, http.MethodDelete, e.wikiAPI("Gone"), e.owner.token, nil, true), http.StatusOK)
	if _, found, _ := e.svc.Code.WikiPageGet(e.owner.name, "proj", "Gone"); found {
		t.Error("page not deleted")
	}
}

func TestWikiRoutes_RenameKeepsContentAndRefusesCollisions(t *testing.T) {
	e := newR2Env(t)
	e.saveWiki(t, e.owner.token, "Old", "original text")
	e.saveWiki(t, e.owner.token, "Taken", "other")

	wantStatus(t, e.form(t, http.MethodPost, e.wikiAPI("Old"), e.owner.token, url.Values{"content": {"x"}, "new_slug": {"Taken"}}, false), http.StatusConflict)
	if c, _, _ := e.svc.Code.WikiPageGet(e.owner.name, "proj", "Taken"); c != "other" {
		t.Errorf("collision overwrote the target: %q", c)
	}

	rr := e.form(t, http.MethodPost, e.wikiAPI("Old"), e.owner.token, url.Values{"content": {"edited text"}, "new_slug": {"Renamed"}}, false)
	wantStatus(t, rr, http.StatusSeeOther)
	if loc := rr.Header().Get("Location"); loc != e.wikiPage("Renamed") {
		t.Errorf("Location = %q, want the new slug", loc)
	}
	if _, found, _ := e.svc.Code.WikiPageGet(e.owner.name, "proj", "Old"); found {
		t.Error("old slug still exists after rename")
	}
	if c, found, _ := e.svc.Code.WikiPageGet(e.owner.name, "proj", "Renamed"); !found || c != "edited text" {
		t.Errorf("renamed page = %q, %v", c, found)
	}

	wantStatus(t, e.form(t, http.MethodPost, e.wikiAPI("Ghost"), e.owner.token, url.Values{"content": {"x"}, "new_slug": {"Elsewhere"}}, false), http.StatusNotFound)
}

func TestWikiRoutes_SetPageOrder(t *testing.T) {
	e := newR2Env(t)
	for _, s := range []string{"Alpha", "Beta", "Gamma"} {
		e.saveWiki(t, e.owner.token, s, s)
	}
	wantStatus(t, e.form(t, http.MethodPost, e.api("/wiki/order"), e.writer.token, url.Values{"order": {" Gamma, ,Alpha,Beta "}}, false), http.StatusOK)
	meta, err := e.svc.Code.WikiPageListMeta(e.owner.name, "proj")
	if err != nil || len(meta) != 3 || meta[0].Slug != "Gamma" || meta[1].Slug != "Alpha" || meta[2].Slug != "Beta" {
		t.Errorf("order = %+v, %v; want Gamma Alpha Beta", meta, err)
	}
	rr := e.form(t, http.MethodPost, e.api("/wiki/order"), e.writer.token, url.Values{"order": {"Alpha,../evil"}}, false)
	wantStatus(t, rr, http.StatusBadRequest)
	bodyHas(t, rr, "invalid page name")
}

func TestWikiRoutes_EditorAndNewPagesNeedWriteAccess(t *testing.T) {
	e := newR2Env(t)
	e.saveWiki(t, e.owner.token, "Doc", "# Doc body")

	rr := e.form(t, http.MethodGet, e.wikiPage("Doc")+"/edit", e.writer.token, nil, false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "# Doc body")
	wantStatus(t, e.form(t, http.MethodGet, e.wikiPage("Doc")+"/edit", e.outsider.token, nil, false), http.StatusForbidden)
	wantStatus(t, e.form(t, http.MethodGet, e.wikiPage("bad.slug")+"/edit", e.writer.token, nil, false), http.StatusBadRequest)
	wantStatus(t, e.form(t, http.MethodGet, e.wikiPage("Doc")+"/edit", "", nil, false), http.StatusSeeOther)

	wantStatus(t, e.form(t, http.MethodGet, "/"+e.owner.name+"/proj/wiki/new", e.writer.token, nil, false), http.StatusOK)
	wantStatus(t, e.form(t, http.MethodGet, "/"+e.owner.name+"/proj/wiki/new", e.outsider.token, nil, false), http.StatusForbidden)
	wantStatus(t, e.form(t, http.MethodGet, "/"+e.owner.name+"/proj/wiki/new", "", nil, false), http.StatusSeeOther)
}

func TestWikiRoutes_PrivateRepoWikiIsHiddenFromStrangers(t *testing.T) {
	e := newR2Env(t)
	if _, err := e.svc.Repo.Create(context.Background(), e.owner.id, e.owner.name, "vault", "", true, service.RepoInitOptions{AddREADME: true}); err != nil {
		t.Fatalf("create private repo: %v", err)
	}
	api := "/api/repos/" + e.owner.name + "/vault/wiki/Home"
	page := "/" + e.owner.name + "/vault/wiki/Home"
	wantStatus(t, e.form(t, http.MethodPost, api, e.owner.token, url.Values{"content": {"secret plans"}}, false), http.StatusSeeOther)

	wantStatus(t, e.form(t, http.MethodGet, page, "", nil, false), http.StatusNotFound)
	wantStatus(t, e.form(t, http.MethodGet, page, e.outsider.token, nil, false), http.StatusNotFound)
	wantStatus(t, e.form(t, http.MethodGet, page+"/edit", e.outsider.token, nil, false), http.StatusNotFound)
	wantStatus(t, e.form(t, http.MethodGet, "/"+e.owner.name+"/vault/wiki/new", e.outsider.token, nil, false), http.StatusNotFound)
	wantStatus(t, e.form(t, http.MethodPost, api, e.outsider.token, url.Values{"content": {"x"}}, false), http.StatusNotFound)
	wantStatus(t, e.form(t, http.MethodDelete, api, e.outsider.token, nil, false), http.StatusNotFound)
	rr := e.form(t, http.MethodGet, page, e.owner.token, nil, false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "secret plans")
}

func TestWikiRoutes_DisabledWikiIsNotFoundEverywhere(t *testing.T) {
	e := newR2Env(t)
	e.saveWiki(t, e.owner.token, "Home", "x")
	e.disableWiki(t)

	wantStatus(t, e.form(t, http.MethodGet, e.wikiPage("Home"), "", nil, false), http.StatusNotFound)
	wantStatus(t, e.form(t, http.MethodGet, e.wikiPage("Home")+"/edit", e.owner.token, nil, false), http.StatusNotFound)
	wantStatus(t, e.form(t, http.MethodGet, "/"+e.owner.name+"/proj/wiki/new", e.owner.token, nil, false), http.StatusNotFound)
	wantStatus(t, e.form(t, http.MethodPost, e.wikiAPI("Home"), e.owner.token, url.Values{"content": {"y"}}, false), http.StatusNotFound)
	wantStatus(t, e.form(t, http.MethodPost, e.api("/wiki/order"), e.owner.token, url.Values{"order": {"Home"}}, false), http.StatusNotFound)
	wantStatus(t, e.form(t, http.MethodDelete, e.wikiAPI("Home"), e.owner.token, nil, false), http.StatusNotFound)
}
