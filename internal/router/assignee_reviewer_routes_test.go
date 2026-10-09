package router_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestIssueAssignees_AddRemove(t *testing.T) {
	e := newMetaEnv(t)
	target := e.path("/issues/1/assignees")
	assigned := func() int {
		return e.count(t, `SELECT COUNT(*) FROM issue_assignees WHERE issue_id = $1`, e.issueID)
	}

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, json: `{"username":"` + e.writer.name + `"}`}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.outsider.token, json: `{"username":"` + e.writer.name + `"}`}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/issues/x/assignees"), token: e.owner.token, json: `{"username":"a"}`}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, json: `{}`}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, json: `not json`}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, json: `{"username":"ghost_nobody"}`}), http.StatusInternalServerError)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/issues/42/assignees"), token: e.owner.token, json: `{"username":"` + e.writer.name + `"}`}), http.StatusInternalServerError)
	if assigned() != 0 {
		t.Fatal("refused requests assigned someone")
	}

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.writer.token, json: `{"username":"` + e.writer.name + `"}`}), http.StatusNoContent)
	rr := e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, htmx: true, form: url.Values{"username": {e.owner.name}}})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, e.owner.name)
	if assigned() != 2 {
		t.Fatalf("assigned = %d, want 2", assigned())
	}

	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target + "?username=" + e.writer.name, token: e.outsider.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target, token: e.owner.token}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/issues/x/assignees?username=a"), token: e.owner.token}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target + "?username=ghost_nobody", token: e.owner.token}), http.StatusInternalServerError)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target + "?username=" + e.writer.name, token: e.owner.token}), http.StatusNoContent)
	rr = e.do(t, metaReq{method: "DELETE", target: target + "?username=" + e.owner.name, token: e.owner.token, htmx: true})
	wantStatus(t, rr, http.StatusOK)
	if assigned() != 0 {
		t.Errorf("assigned = %d after removal", assigned())
	}
}

func TestPullAssignees_AddRemoveRecordTimeline(t *testing.T) {
	e := newMetaEnv(t)
	target := e.path("/pulls/1/assignees")
	assigned := func() int {
		return e.count(t, `SELECT COUNT(*) FROM pull_assignees WHERE pull_id = $1`, e.pullID)
	}

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, json: `{"username":"a"}`}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.outsider.token, json: `{"username":"` + e.writer.name + `"}`}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/pulls/x/assignees"), token: e.owner.token, json: `{"username":"a"}`}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, json: `{}`}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, json: `{"username":"ghost_nobody"}`}), http.StatusInternalServerError)
	if assigned() != 0 {
		t.Fatal("refused requests assigned someone")
	}

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.writer.token, json: `{"username":"` + e.writer.name + `"}`}), http.StatusNoContent)
	rr := e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, htmx: true, form: url.Values{"username": {e.owner.name}}})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, e.owner.name)
	if assigned() != 2 {
		t.Fatalf("assigned = %d, want 2", assigned())
	}
	if n := e.count(t, `SELECT COUNT(*) FROM pull_events WHERE pull_id = $1 AND event_type = 'assigned'`, e.pullID); n != 2 {
		t.Errorf("assigned events = %d, want 2", n)
	}

	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target + "?username=" + e.writer.name, token: e.outsider.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target, token: e.owner.token}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/pulls/x/assignees?username=a"), token: e.owner.token}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target + "?username=ghost_nobody", token: e.owner.token}), http.StatusInternalServerError)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target + "?username=" + e.writer.name, token: e.owner.token}), http.StatusNoContent)
	rr = e.do(t, metaReq{method: "DELETE", target: target + "?username=" + e.owner.name, token: e.owner.token, htmx: true})
	wantStatus(t, rr, http.StatusOK)
	if assigned() != 0 {
		t.Errorf("assigned = %d after removal", assigned())
	}
	if n := e.count(t, `SELECT COUNT(*) FROM pull_events WHERE pull_id = $1 AND event_type = 'unassigned'`, e.pullID); n != 2 {
		t.Errorf("unassigned events = %d, want 2", n)
	}
}

func TestPullReviewers_RequestAndWithdraw(t *testing.T) {
	e := newMetaEnv(t)
	target := e.path("/pulls/1/reviewers")
	pending := func() int {
		return e.count(t, `SELECT COUNT(*) FROM pull_reviews WHERE pull_id = $1`, e.pullID)
	}
	form := func(name string) url.Values { return url.Values{"username": {name}} }

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, form: form(e.writer.name)}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.outsider.token, form: form(e.writer.name)}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/pulls/x/reviewers"), token: e.owner.token, form: form(e.writer.name)}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, form: form("")}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, form: form("ghost_nobody")}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/pulls/9/reviewers"), token: e.owner.token, form: form(e.writer.name)}), http.StatusInternalServerError)
	if pending() != 0 {
		t.Fatal("refused requests requested a review")
	}

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, form: form(e.writer.name)}), http.StatusNoContent)
	if pending() != 1 {
		t.Fatalf("reviews = %d, want 1", pending())
	}
	rr := e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, htmx: true, form: form(e.outsider.name)})
	wantStatus(t, rr, http.StatusNoContent)
	if rr.Header().Get("HX-Trigger") == "" {
		t.Error("no toast header")
	}

	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target + "?username=" + e.writer.name, token: e.outsider.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target, token: e.owner.token}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/pulls/x/reviewers?username=a"), token: e.owner.token}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target + "?username=ghost_nobody", token: e.owner.token}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/pulls/9/reviewers?username=%s", e.writer.name), token: e.owner.token}), http.StatusInternalServerError)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target + "?username=" + e.writer.name, token: e.owner.token}), http.StatusNoContent)
	if pending() != 1 {
		t.Fatalf("reviews = %d, want 1 left", pending())
	}
	rr = e.do(t, metaReq{method: "DELETE", target: target + "?username=" + e.outsider.name, token: e.owner.token, htmx: true})
	wantStatus(t, rr, http.StatusNoContent)
	if pending() != 0 {
		t.Errorf("reviews = %d after withdrawal", pending())
	}
}

func TestPullReviewers_CannotRequestAUserWhoCannotReadThePrivateRepo(t *testing.T) {
	e := newMetaEnv(t)
	testutil.Exec(t, e.db, `UPDATE repositories SET private = true WHERE id = $1`, e.repoID)

	rr := e.do(t, metaReq{method: "POST", target: e.path("/pulls/1/reviewers"), token: e.owner.token, form: url.Values{"username": {e.outsider.name}}})
	wantStatus(t, rr, http.StatusForbidden)
	if n := e.count(t, `SELECT COUNT(*) FROM pull_reviews WHERE pull_id = $1`, e.pullID); n != 0 {
		t.Errorf("reviews = %d, want 0", n)
	}

	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/pulls/1/reviewers"), token: e.outsider.token, form: url.Values{"username": {e.writer.name}}}), http.StatusNotFound)
}
