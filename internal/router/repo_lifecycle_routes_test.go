package router_test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func (e r2Env) repoFlag(t *testing.T, column string) bool {
	t.Helper()
	var v bool
	if err := e.db.QueryRow(`SELECT `+column+` FROM repositories WHERE id = $1`, e.repo.ID).Scan(&v); err != nil {
		t.Fatalf("read %s: %v", column, err)
	}
	return v
}

func TestRepoLifecycleRoutes_ArchiveAndUnarchiveAreOwnerOnly(t *testing.T) {
	e := newR2Env(t)

	// An admin can manage the repo but archiving is reserved for its owner.
	for _, tok := range []string{e.admin.token, e.writer.token} {
		wantStatus(t, e.form(t, http.MethodPost, e.api("/archive"), tok, nil, true), http.StatusForbidden)
	}
	wantStatus(t, e.form(t, http.MethodPost, e.api("/archive"), e.outsider.token, nil, true), http.StatusForbidden)
	wantStatus(t, e.form(t, http.MethodPost, e.api("/archive"), "", nil, true), http.StatusUnauthorized)
	if e.repoFlag(t, "is_archived") {
		t.Fatal("a refused request archived the repo")
	}

	rr := e.form(t, http.MethodPost, e.api("/archive"), e.owner.token, nil, true)
	wantStatus(t, rr, http.StatusNoContent)
	if got := rr.Header().Get("HX-Redirect"); got != "/"+e.owner.name+"/proj/settings" {
		t.Errorf("HX-Redirect = %q", got)
	}
	if !e.repoFlag(t, "is_archived") {
		t.Fatal("repo not archived")
	}

	wantStatus(t, e.form(t, http.MethodPost, e.api("/unarchive"), e.admin.token, nil, true), http.StatusForbidden)
	if !e.repoFlag(t, "is_archived") {
		t.Fatal("admin unarchived the repo")
	}
	wantStatus(t, e.form(t, http.MethodPost, e.api("/unarchive"), e.owner.token, nil, true), http.StatusNoContent)
	if e.repoFlag(t, "is_archived") {
		t.Error("repo still archived")
	}
}

func TestRepoLifecycleRoutes_ArchiveHidesPrivateRepoFromStrangers(t *testing.T) {
	e := newR2Env(t)
	if _, err := e.svc.Repo.Create(context.Background(), e.owner.id, e.owner.name, "hidden", "", true, service.RepoInitOptions{AddREADME: true}); err != nil {
		t.Fatalf("create: %v", err)
	}
	rr := e.form(t, http.MethodPost, "/api/repos/"+e.owner.name+"/hidden/archive", e.outsider.token, nil, true)
	wantStatus(t, rr, http.StatusNotFound)
}

func TestRepoLifecycleRoutes_TemplateToggle(t *testing.T) {
	e := newR2Env(t)

	wantStatus(t, e.form(t, http.MethodPatch, e.api("/template"), e.admin.token, url.Values{"is_template": {"true"}}, true), http.StatusForbidden)
	if e.repoFlag(t, "is_template") {
		t.Fatal("admin toggled the template flag")
	}

	for _, v := range []string{"true", "on"} {
		wantStatus(t, e.form(t, http.MethodPatch, e.api("/template"), e.owner.token, url.Values{"is_template": {v}}, true), http.StatusNoContent)
		if !e.repoFlag(t, "is_template") {
			t.Errorf("is_template=%q did not mark the repo", v)
		}
		wantStatus(t, e.form(t, http.MethodPatch, e.api("/template"), e.owner.token, url.Values{}, true), http.StatusNoContent)
		if e.repoFlag(t, "is_template") {
			t.Error("a missing is_template did not clear the flag")
		}
	}
}

func TestRepoLifecycleRoutes_CreateFromTemplate(t *testing.T) {
	e := newR2Env(t)
	idStr := strconv.FormatInt(e.repo.ID, 10)
	post := func(token string, f url.Values) int {
		return e.form(t, http.MethodPost, "/api/repos/from-template", token, f, false).Code
	}

	if got := post(e.outsider.token, url.Values{"template_repo_id": {idStr}, "name": {"copy"}}); got != http.StatusUnprocessableEntity {
		t.Errorf("non-template source = %d, want 422", got)
	}
	wantStatus(t, e.form(t, http.MethodPatch, e.api("/template"), e.owner.token, url.Values{"is_template": {"true"}}, true), http.StatusNoContent)

	if got := post(e.outsider.token, url.Values{"name": {"copy"}}); got != http.StatusBadRequest {
		t.Errorf("missing template id = %d, want 400", got)
	}
	if got := post(e.outsider.token, url.Values{"template_repo_id": {"junk"}, "name": {"copy"}}); got != http.StatusBadRequest {
		t.Errorf("non-numeric template id = %d, want 400", got)
	}
	if got := post(e.outsider.token, url.Values{"template_repo_id": {idStr}}); got != http.StatusBadRequest {
		t.Errorf("missing name = %d, want 400", got)
	}
	if got := post(e.outsider.token, url.Values{"template_repo_id": {idStr}, "name": {"bad name!"}}); got != http.StatusUnprocessableEntity {
		t.Errorf("invalid name = %d, want 422", got)
	}
	if got := post(e.outsider.token, url.Values{"template_repo_id": {idStr}, "name": {"copy.wiki"}}); got != http.StatusUnprocessableEntity {
		t.Errorf("reserved name = %d, want 422", got)
	}
	if got := post(e.outsider.token, url.Values{"template_repo_id": {"999999999"}, "name": {"copy"}}); got != http.StatusNotFound {
		t.Errorf("unknown template = %d, want 404", got)
	}
	if got := post("", url.Values{"template_repo_id": {idStr}, "name": {"copy"}}); got != http.StatusUnauthorized {
		t.Errorf("anonymous = %d, want 401", got)
	}

	rr := e.form(t, http.MethodPost, "/api/repos/from-template", e.outsider.token, url.Values{"template_repo_id": {idStr}, "name": {"copy"}, "description": {"from tmpl"}}, false)
	wantStatus(t, rr, http.StatusSeeOther)
	if loc := rr.Header().Get("Location"); loc != "/"+e.outsider.name+"/copy" {
		t.Errorf("Location = %q", loc)
	}
	copied, err := e.svc.Repo.Get(context.Background(), e.outsider.name, "copy")
	if err != nil || copied.Description != "from tmpl" || copied.Private {
		t.Fatalf("copy = %+v, %v", copied, err)
	}
	t.Cleanup(func() { testutil.Exec(t, e.db, `DELETE FROM repositories WHERE id = $1`, copied.ID) })
	if _, _, err := e.svc.Code.ResolveRef(e.outsider.name, "copy", "main"); err != nil {
		t.Errorf("copy has no content: %v", err)
	}

	if got := post(e.outsider.token, url.Values{"template_repo_id": {idStr}, "name": {"copy"}}); got != http.StatusUnprocessableEntity {
		t.Errorf("a second copy under the same name = %d, want 422", got)
	}

	wantStatus(t, e.form(t, http.MethodPost, e.api("/archive"), e.owner.token, nil, true), http.StatusNoContent)
	if got := post(e.outsider.token, url.Values{"template_repo_id": {idStr}, "name": {"copy2"}}); got != http.StatusUnprocessableEntity {
		t.Errorf("archived template = %d, want 422", got)
	}
}

func TestRepoLifecycleRoutes_DeleteNeedsOwnerAndPassword(t *testing.T) {
	e := newR2Env(t)
	target := e.api("/delete")

	wantStatus(t, e.form(t, http.MethodPost, target, e.admin.token, url.Values{"password": {r2Password}}, true), http.StatusForbidden)
	wantStatus(t, e.form(t, http.MethodPost, "/api/repos/"+e.owner.name+"/nope/delete", e.owner.token, nil, true), http.StatusNotFound)

	rr := e.form(t, http.MethodPost, target, e.owner.token, url.Values{"password": {"wrong"}}, true)
	wantStatus(t, rr, http.StatusForbidden)
	rr = e.form(t, http.MethodPost, target, e.owner.token, nil, true)
	wantStatus(t, rr, http.StatusForbidden)
	if _, err := e.svc.Repo.Get(context.Background(), e.owner.name, "proj"); err != nil {
		t.Fatalf("unconfirmed delete removed the repo: %v", err)
	}

	rr = e.form(t, http.MethodPost, target, e.owner.token, url.Values{"password": {r2Password}}, true)
	wantStatus(t, rr, http.StatusNoContent)
	if got := rr.Header().Get("HX-Redirect"); got != "/"+e.owner.name {
		t.Errorf("HX-Redirect = %q", got)
	}
	if _, err := e.svc.Repo.Get(context.Background(), e.owner.name, "proj"); err == nil {
		t.Error("repo still resolvable after delete")
	}
}

func TestRepoLifecycleRoutes_PlainDeleteRedirectsToOwner(t *testing.T) {
	e := newR2Env(t)
	rr := e.form(t, http.MethodPost, e.api("/delete"), e.owner.token, url.Values{"password": {r2Password}}, false)
	wantStatus(t, rr, http.StatusSeeOther)
	if loc := rr.Header().Get("Location"); loc != "/"+e.owner.name {
		t.Errorf("Location = %q", loc)
	}
}
