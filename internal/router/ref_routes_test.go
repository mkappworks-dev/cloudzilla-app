package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestBranchRoutes_CreateAndDelete(t *testing.T) {
	e := newR2Env(t)

	rr := e.form(t, http.MethodPost, e.api("/branches"), e.writer.token, url.Values{"name": {"feature/x"}}, false)
	wantStatus(t, rr, http.StatusCreated)
	if got := r2DecodeJSON[map[string]string](t, rr)["name"]; got != "feature/x" {
		t.Errorf("name = %q", got)
	}
	if !e.hasRef(t, "branch", "feature/x") {
		t.Fatal("branch not created")
	}

	rr = e.form(t, http.MethodDelete, e.api("/branches?name=feature/x"), e.writer.token, nil, false)
	wantStatus(t, rr, http.StatusOK)
	if e.hasRef(t, "branch", "feature/x") {
		t.Error("branch still exists after delete")
	}
}

func TestBranchRoutes_CreateRefusals(t *testing.T) {
	e := newR2Env(t)
	post := func(token string, f url.Values) *httptest.ResponseRecorder {
		return e.form(t, http.MethodPost, e.api("/branches"), token, f, false)
	}

	wantStatus(t, post(e.writer.token, url.Values{}), http.StatusBadRequest)
	wantStatus(t, post(e.writer.token, url.Values{"name": {"b"}, "from": {"nope"}}), http.StatusBadRequest)
	wantStatus(t, post(e.writer.token, url.Values{"name": {e.repo.DefaultBranch}}), http.StatusBadRequest)
	wantStatus(t, post(e.outsider.token, url.Values{"name": {"b"}}), http.StatusForbidden)
	wantStatus(t, post("", url.Values{"name": {"b"}}), http.StatusUnauthorized)
	if e.hasRef(t, "branch", "b") {
		t.Error("a refused request created the branch")
	}
}

func TestBranchRoutes_ProtectedBranchNameCannotBeCreatedOrDeleted(t *testing.T) {
	e := newR2Env(t)
	e.exec(t, `INSERT INTO branch_protections (repo_id, pattern, require_pull_request) VALUES ($1, 'release', true)`)
	e.exec(t, `INSERT INTO branch_protections (repo_id, pattern, block_force_push) VALUES ($1, 'keep', true)`)

	rr := e.form(t, http.MethodPost, e.api("/branches"), e.writer.token, url.Values{"name": {"release"}}, false)
	wantStatus(t, rr, http.StatusUnprocessableEntity)
	if e.hasRef(t, "branch", "release") {
		t.Error("PR-only branch was created from the browser")
	}

	wantStatus(t, e.form(t, http.MethodPost, e.api("/branches"), e.writer.token, url.Values{"name": {"keep"}}, false), http.StatusCreated)
	rr = e.form(t, http.MethodDelete, e.api("/branches?name=keep"), e.writer.token, nil, false)
	wantStatus(t, rr, http.StatusUnprocessableEntity)
	if !e.hasRef(t, "branch", "keep") {
		t.Error("branch whose rule blocks force pushes was deleted")
	}
}

func TestBranchRoutes_DeleteRefusals(t *testing.T) {
	e := newR2Env(t)
	del := func(token, query string) *httptest.ResponseRecorder {
		return e.form(t, http.MethodDelete, e.api("/branches"+query), token, nil, false)
	}
	wantStatus(t, del(e.writer.token, ""), http.StatusBadRequest)
	rr := del(e.writer.token, "?name="+e.repo.DefaultBranch)
	wantStatus(t, rr, http.StatusBadRequest)
	if r2ErrorMessage(t, rr) != "cannot delete the default branch" {
		t.Errorf("error = %q", r2ErrorMessage(t, rr))
	}
	wantStatus(t, del(e.outsider.token, "?name=x"), http.StatusForbidden)
	if !e.hasRef(t, "branch", e.repo.DefaultBranch) {
		t.Error("default branch is gone")
	}
}

func TestBranchRoutes_HTMXRendersTheBranchList(t *testing.T) {
	e := newR2Env(t)
	rr := e.form(t, http.MethodPost, e.api("/branches"), e.writer.token, url.Values{"name": {"htmx-branch"}}, true)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "htmx-branch")

	rr = e.form(t, http.MethodDelete, e.api("/branches?name=htmx-branch"), e.writer.token, nil, true)
	wantStatus(t, rr, http.StatusOK)
	if strings.Contains(rr.Body.String(), "htmx-branch") {
		t.Errorf("deleted branch still listed: %.300s", rr.Body.String())
	}
}

func TestBranchRoutes_ArchivedRepoIsReadOnly(t *testing.T) {
	e := newR2Env(t)
	if err := e.svc.Repo.Archive(context.Background(), e.repo.ID, e.owner.id); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	rr := e.form(t, http.MethodPost, e.api("/branches"), e.owner.token, url.Values{"name": {"late"}}, false)
	wantStatus(t, rr, http.StatusForbidden)
	rr = e.form(t, http.MethodPost, e.api("/tags"), e.owner.token, url.Values{"name": {"late"}}, false)
	wantStatus(t, rr, http.StatusForbidden)
}

func TestTagRoutes_CreateAndDelete(t *testing.T) {
	e := newR2Env(t)

	rr := e.form(t, http.MethodPost, e.api("/tags"), e.writer.token, url.Values{"name": {"v1.0"}}, false)
	wantStatus(t, rr, http.StatusCreated)
	if !e.hasRef(t, "tag", "v1.0") {
		t.Fatal("tag not created")
	}
	wantStatus(t, e.form(t, http.MethodPost, e.api("/tags"), e.writer.token, url.Values{"name": {"v1.0"}}, false), http.StatusBadRequest)

	rr = e.form(t, http.MethodDelete, e.api("/tags?name=v1.0"), e.writer.token, nil, false)
	wantStatus(t, rr, http.StatusOK)
	if e.hasRef(t, "tag", "v1.0") {
		t.Error("tag still exists after delete")
	}
}

func TestTagRoutes_Refusals(t *testing.T) {
	e := newR2Env(t)
	wantStatus(t, e.form(t, http.MethodPost, e.api("/tags"), e.writer.token, url.Values{}, false), http.StatusBadRequest)
	wantStatus(t, e.form(t, http.MethodPost, e.api("/tags"), e.writer.token, url.Values{"name": {"t"}, "from": {"missing"}}, false), http.StatusBadRequest)
	wantStatus(t, e.form(t, http.MethodPost, e.api("/tags"), e.outsider.token, url.Values{"name": {"t"}}, false), http.StatusForbidden)
	wantStatus(t, e.form(t, http.MethodDelete, e.api("/tags"), e.writer.token, nil, false), http.StatusBadRequest)
	wantStatus(t, e.form(t, http.MethodDelete, e.api("/tags?name=t"), e.outsider.token, nil, false), http.StatusForbidden)
}

func TestTagRoutes_HTMXRendersTheTagList(t *testing.T) {
	e := newR2Env(t)
	from := e.repo.DefaultBranch
	rr := e.form(t, http.MethodPost, e.api("/tags"), e.writer.token, url.Values{"name": {"v2"}, "from": {from}}, true)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "v2")
	rr = e.form(t, http.MethodDelete, e.api("/tags?name=v2"), e.writer.token, nil, true)
	wantStatus(t, rr, http.StatusOK)
}
