package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

func (e r2Env) headSHA(t *testing.T) string {
	t.Helper()
	commit, _, err := e.svc.Code.ResolveRef(e.owner.name, "proj", e.repo.DefaultBranch)
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}
	return commit.Hash.String()
}

func TestCommitStatusRoutes_PostThenListAndCombine(t *testing.T) {
	e := newR2Env(t)
	sha := e.headSHA(t)
	post := func(token, body string) *httptest.ResponseRecorder {
		return e.jsonBody(t, http.MethodPost, e.api("/statuses/"+sha), token, body)
	}

	rr := post(e.writer.token, `{"state":"success","context":"ci/build","target_url":"https://ci.example/1","description":"ok"}`)
	wantStatus(t, rr, http.StatusCreated)
	cs := r2DecodeJSON[model.CommitStatus](t, rr)
	if cs.SHA != sha || cs.Context != "ci/build" || cs.State != model.CommitStatusSuccess || cs.CreatorID != e.writer.id {
		t.Errorf("created = %+v", cs)
	}

	// An omitted context lands on "default"; re-posting a context replaces its state.
	wantStatus(t, post(e.writer.token, `{"state":"pending"}`), http.StatusCreated)
	wantStatus(t, post(e.writer.token, `{"state":"failure","context":"ci/build"}`), http.StatusCreated)

	rr = e.form(t, http.MethodGet, e.api("/statuses/"+sha), "", nil, false)
	wantStatus(t, rr, http.StatusOK)
	list := r2DecodeJSON[[]model.CommitStatus](t, rr)
	if len(list) != 2 || list[0].Context != "ci/build" || list[0].State != model.CommitStatusFailure || list[1].Context != "default" {
		t.Errorf("list = %+v, want ci/build=failure then default=pending", list)
	}

	rr = e.form(t, http.MethodGet, e.api("/commits/"+sha+"/status"), "", nil, false)
	wantStatus(t, rr, http.StatusOK)
	combined := r2DecodeJSON[struct {
		State    string
		Statuses []model.CommitStatus
	}](t, rr)
	if combined.State != "failure" || len(combined.Statuses) != 2 {
		t.Errorf("combined = %+v, want failure over pending", combined)
	}
}

func TestCommitStatusRoutes_EmptyListsAreArrays(t *testing.T) {
	e := newR2Env(t)
	rr := e.form(t, http.MethodGet, e.api("/statuses/"+e.headSHA(t)), "", nil, false)
	wantStatus(t, rr, http.StatusOK)
	if got := rr.Body.String(); got != "[]\n" && got != "[]" {
		t.Errorf("empty list body = %q, want []", got)
	}
	rr = e.form(t, http.MethodGet, e.api("/commits/"+e.headSHA(t)+"/status"), "", nil, false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, `"statuses":[]`)
}

func TestCommitStatusRoutes_PostRefusals(t *testing.T) {
	e := newR2Env(t)
	sha := e.headSHA(t)
	post := func(token, body string) *httptest.ResponseRecorder {
		return e.jsonBody(t, http.MethodPost, e.api("/statuses/"+sha), token, body)
	}

	wantStatus(t, post(e.writer.token, `not json`), http.StatusBadRequest)
	wantStatus(t, post(e.writer.token, `{"state":"great"}`), http.StatusBadRequest)
	wantStatus(t, post(e.writer.token, `{}`), http.StatusBadRequest)
	for _, u := range []string{"javascript:alert(1)", "ftp://x/y", "//evil.test/x", "not a url"} {
		wantStatus(t, post(e.writer.token, `{"state":"success","target_url":"`+u+`"}`), http.StatusBadRequest)
	}
	wantStatus(t, post(e.outsider.token, `{"state":"success"}`), http.StatusForbidden)
	wantStatus(t, post("", `{"state":"success"}`), http.StatusUnauthorized)

	rr := e.form(t, http.MethodGet, e.api("/statuses/"+sha), "", nil, false)
	if list := r2DecodeJSON[[]model.CommitStatus](t, rr); len(list) != 0 {
		t.Errorf("refused posts stored statuses: %+v", list)
	}
}

func TestCommitStatusRoutes_PrivateRepoStatusesAreHidden(t *testing.T) {
	e := newR2Env(t)
	if _, err := e.svc.Repo.Create(context.Background(), e.owner.id, e.owner.name, "secret", "", true, service.RepoInitOptions{AddREADME: true}); err != nil {
		t.Fatalf("create private repo: %v", err)
	}
	commit, _, err := e.svc.Code.ResolveRef(e.owner.name, "secret", "main")
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}
	base := "/api/repos/" + e.owner.name + "/secret"
	sha := commit.Hash.String()

	wantStatus(t, e.jsonBody(t, http.MethodPost, base+"/statuses/"+sha, e.owner.token, `{"state":"success"}`), http.StatusCreated)
	for _, target := range []string{base + "/statuses/" + sha, base + "/commits/" + sha + "/status"} {
		wantStatus(t, e.form(t, http.MethodGet, target, "", nil, false), http.StatusNotFound)
		wantStatus(t, e.form(t, http.MethodGet, target, e.outsider.token, nil, false), http.StatusNotFound)
		wantStatus(t, e.form(t, http.MethodGet, target, e.owner.token, nil, false), http.StatusOK)
	}
}
