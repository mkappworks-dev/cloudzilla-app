package router_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func (e metaEnv) seedLabel(t *testing.T, name string) int64 {
	t.Helper()
	var id int64
	if err := e.db.QueryRow(`INSERT INTO labels (repo_id, name) VALUES ($1, $2) RETURNING id`, e.repoID, name).Scan(&id); err != nil {
		t.Fatalf("seed label: %v", err)
	}
	return id
}

func TestLabels_List(t *testing.T) {
	e := newMetaEnv(t)
	e.seedLabel(t, "bug")

	rr := e.do(t, metaReq{method: "GET", target: e.path("/labels")})
	wantStatus(t, rr, http.StatusOK)
	var labels []struct{ Name string }
	if err := json.Unmarshal(rr.Body.Bytes(), &labels); err != nil || len(labels) != 1 || labels[0].Name != "bug" {
		t.Errorf("labels = %s (%v)", rr.Body.String(), err)
	}

	rr = e.do(t, metaReq{method: "GET", target: e.path("/labels"), token: e.owner.token, htmx: true})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "bug")

	wantStatus(t, e.do(t, metaReq{method: "GET", target: "/api/repos/" + e.owner.name + "/nope/labels"}), http.StatusNotFound)
}

func TestLabels_ListEmptyIsAnArray(t *testing.T) {
	e := newMetaEnv(t)
	rr := e.do(t, metaReq{method: "GET", target: e.path("/labels")})
	wantStatus(t, rr, http.StatusOK)
	if got := rr.Body.String(); got != "[]\n" && got != "[]" {
		t.Errorf("body = %q, want an empty array", got)
	}
}

func TestLabels_Create(t *testing.T) {
	e := newMetaEnv(t)

	rr := e.do(t, metaReq{method: "POST", target: e.path("/labels"), token: e.writer.token, json: `{"name":"feat","color":"#abc","description":"d"}`})
	wantStatus(t, rr, http.StatusCreated)
	var color string
	if err := e.db.QueryRow(`SELECT color FROM labels WHERE repo_id = $1 AND name = 'feat'`, e.repoID).Scan(&color); err != nil || color != "#abc" {
		t.Errorf("stored color = %q (%v)", color, err)
	}

	rr = e.do(t, metaReq{method: "POST", target: e.path("/labels"), token: e.owner.token, json: `{"name":"plain"}`})
	wantStatus(t, rr, http.StatusCreated)
	if err := e.db.QueryRow(`SELECT color FROM labels WHERE repo_id = $1 AND name = 'plain'`, e.repoID).Scan(&color); err != nil || color != "#e5e5e5" {
		t.Errorf("default color = %q (%v)", color, err)
	}

	rr = e.do(t, metaReq{method: "POST", target: e.path("/labels"), token: e.owner.token, htmx: true,
		form: url.Values{"name": {"docs"}, "color": {"#aabbcc"}, "description": {"x"}}})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "docs")
	if n := e.count(t, `SELECT COUNT(*) FROM labels WHERE repo_id = $1`, e.repoID); n != 3 {
		t.Errorf("labels = %d, want 3", n)
	}
}

func TestLabels_CreateRefusals(t *testing.T) {
	e := newMetaEnv(t)
	cases := []struct {
		name string
		req  metaReq
		want int
	}{
		{"anonymous", metaReq{method: "POST", target: e.path("/labels"), json: `{"name":"a"}`}, http.StatusUnauthorized},
		{"outsider", metaReq{method: "POST", target: e.path("/labels"), token: e.outsider.token, json: `{"name":"a"}`}, http.StatusForbidden},
		{"bad json", metaReq{method: "POST", target: e.path("/labels"), token: e.owner.token, json: `{`}, http.StatusBadRequest},
		{"bad color", metaReq{method: "POST", target: e.path("/labels"), token: e.owner.token, json: `{"name":"a","color":"red"}`}, http.StatusBadRequest},
		{"bad color htmx", metaReq{method: "POST", target: e.path("/labels"), token: e.owner.token, htmx: true, form: url.Values{"name": {"a"}, "color": {"#12"}}}, http.StatusBadRequest},
		{"unknown repo", metaReq{method: "POST", target: "/api/repos/" + e.owner.name + "/nope/labels", token: e.owner.token, json: `{"name":"a"}`}, http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantStatus(t, e.do(t, c.req), c.want) })
	}
	if n := e.count(t, `SELECT COUNT(*) FROM labels WHERE repo_id = $1`, e.repoID); n != 0 {
		t.Errorf("refused requests created %d labels", n)
	}

	e.seedLabel(t, "dup")
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/labels"), token: e.owner.token, json: `{"name":"dup"}`}), http.StatusInternalServerError)
}

func TestLabels_Delete(t *testing.T) {
	e := newMetaEnv(t)
	a, b := e.seedLabel(t, "a"), e.seedLabel(t, "b")

	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/labels/%d", a)}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/labels/%d", a), token: e.outsider.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/labels/x"), token: e.owner.token}), http.StatusBadRequest)
	if n := e.count(t, `SELECT COUNT(*) FROM labels WHERE repo_id = $1`, e.repoID); n != 2 {
		t.Fatalf("refusals changed labels: %d", n)
	}

	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/labels/%d", a), token: e.writer.token}), http.StatusNoContent)
	rr := e.do(t, metaReq{method: "DELETE", target: e.path("/labels/%d", b), token: e.owner.token, htmx: true})
	wantStatus(t, rr, http.StatusOK)
	if n := e.count(t, `SELECT COUNT(*) FROM labels WHERE repo_id = $1`, e.repoID); n != 0 {
		t.Errorf("labels left = %d", n)
	}
}

func TestLabels_DeleteOtherReposLabelLeavesItAlone(t *testing.T) {
	e := newMetaEnv(t)
	otherSfx := testutil.UniqueSuffix(t)
	otherRepo := testutil.SeedRepo(t, e.db, e.owner.id, e.owner.name, otherSfx)
	var foreign int64
	if err := e.db.QueryRow(`INSERT INTO labels (repo_id, name) VALUES ($1, 'x') RETURNING id`, otherRepo).Scan(&foreign); err != nil {
		t.Fatal(err)
	}
	e.do(t, metaReq{method: "DELETE", target: e.path("/labels/%d", foreign), token: e.owner.token})
	if n := e.count(t, `SELECT COUNT(*) FROM labels WHERE id = $1`, foreign); n != 1 {
		t.Error("label of another repo was deleted through this repo's URL")
	}
}

func TestIssueLabels_AddRemove(t *testing.T) {
	e := newMetaEnv(t)
	id := e.seedLabel(t, "bug")
	target := e.path("/issues/1/labels/%d", id)
	attached := func() int {
		return e.count(t, `SELECT COUNT(*) FROM issue_labels WHERE issue_id = $1`, e.issueID)
	}

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.outsider.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/issues/x/labels/%d", id), token: e.owner.token}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/issues/1/labels/x"), token: e.owner.token}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/issues/1/labels/999999999"), token: e.owner.token}), http.StatusInternalServerError)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/issues/42/labels/%d", id), token: e.owner.token}), http.StatusInternalServerError)
	if attached() != 0 {
		t.Fatal("refused requests attached a label")
	}

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.writer.token}), http.StatusNoContent)
	if attached() != 1 {
		t.Fatal("label not attached")
	}
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target, token: e.outsider.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/issues/1/labels/x"), token: e.owner.token}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/issues/x/labels/%d", id), token: e.owner.token}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/issues/42/labels/%d", id), token: e.owner.token}), http.StatusInternalServerError)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target, token: e.owner.token}), http.StatusNoContent)
	if attached() != 0 {
		t.Error("label not detached")
	}
}

func TestIssueLabels_HTMXRendersSidebar(t *testing.T) {
	e := newMetaEnv(t)
	id := e.seedLabel(t, "sidebarlabel")
	target := e.path("/issues/1/labels/%d", id)

	rr := e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, htmx: true})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "sidebarlabel")
	if rr.Header().Get("HX-Trigger") == "" {
		t.Error("no toast header")
	}
	rr = e.do(t, metaReq{method: "DELETE", target: target, token: e.owner.token, htmx: true})
	wantStatus(t, rr, http.StatusOK)
	if n := e.count(t, `SELECT COUNT(*) FROM issue_labels WHERE issue_id = $1`, e.issueID); n != 0 {
		t.Errorf("attached = %d", n)
	}
}

func TestPullLabels_AddRemoveRecordTimeline(t *testing.T) {
	e := newMetaEnv(t)
	id := e.seedLabel(t, "ready")
	target := e.path("/pulls/1/labels/%d", id)
	events := func() int {
		return e.count(t, `SELECT COUNT(*) FROM pull_events WHERE pull_id = $1`, e.pullID)
	}

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.outsider.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/pulls/x/labels/%d", id), token: e.owner.token}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/pulls/1/labels/x"), token: e.owner.token}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/pulls/9/labels/%d", id), token: e.owner.token}), http.StatusInternalServerError)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/pulls/1/labels/999999999"), token: e.owner.token}), http.StatusInternalServerError)
	if events() != 0 {
		t.Fatal("refused requests recorded events")
	}

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.writer.token}), http.StatusNoContent)
	if n := e.count(t, `SELECT COUNT(*) FROM pull_labels WHERE pull_id = $1`, e.pullID); n != 1 {
		t.Fatalf("pull labels = %d", n)
	}
	var detail, actor string
	if err := e.db.QueryRow(`SELECT detail, actor_name FROM pull_events WHERE pull_id = $1`, e.pullID).Scan(&detail, &actor); err != nil || detail != "ready" || actor != e.writer.name {
		t.Errorf("event detail=%q actor=%q (%v)", detail, actor, err)
	}

	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target, token: e.outsider.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/pulls/x/labels/%d", id), token: e.owner.token}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/pulls/1/labels/x"), token: e.owner.token}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/pulls/9/labels/%d", id), token: e.owner.token}), http.StatusInternalServerError)
	rr := e.do(t, metaReq{method: "DELETE", target: target, token: e.owner.token, htmx: true})
	wantStatus(t, rr, http.StatusOK)
	if n := e.count(t, `SELECT COUNT(*) FROM pull_labels WHERE pull_id = $1`, e.pullID); n != 0 {
		t.Errorf("pull labels = %d after remove", n)
	}
	if events() != 2 {
		t.Errorf("events = %d, want labeled + unlabeled", events())
	}

	rr = e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, htmx: true})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "ready")
}

func TestDiscussionLabels_AddRemove(t *testing.T) {
	e := newMetaEnv(t)
	id := e.seedLabel(t, "question")
	target := e.path("/discussions/1/labels/%d", id)
	attached := func() int {
		return e.count(t, `SELECT COUNT(*) FROM discussion_labels WHERE discussion_id = $1`, e.discussionID)
	}

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.outsider.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/discussions/x/labels/%d", id), token: e.owner.token}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/discussions/1/labels/x"), token: e.owner.token}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/discussions/9/labels/%d", id), token: e.owner.token}), http.StatusInternalServerError)
	if attached() != 0 {
		t.Fatal("refused requests attached a label")
	}

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.writer.token}), http.StatusNoContent)
	if attached() != 1 {
		t.Fatal("label not attached")
	}
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target, token: e.outsider.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/discussions/x/labels/%d", id), token: e.owner.token}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/discussions/1/labels/x"), token: e.owner.token}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/discussions/9/labels/%d", id), token: e.owner.token}), http.StatusInternalServerError)
	rr := e.do(t, metaReq{method: "DELETE", target: target, token: e.owner.token, htmx: true})
	wantStatus(t, rr, http.StatusOK)
	if attached() != 0 {
		t.Error("label not detached")
	}

	rr = e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, htmx: true})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "question")
}
