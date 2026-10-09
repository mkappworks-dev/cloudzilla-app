package router_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func (e metaEnv) seedMilestone(t *testing.T, title string) (id int64, number int) {
	t.Helper()
	m, err := e.svc.Milestone.Create(context.Background(), e.owner.name, e.repoName, title, "desc of "+title, nil)
	if err != nil {
		t.Fatalf("seed milestone: %v", err)
	}
	return m.ID, m.Number
}

func (e metaEnv) milestoneCol(t *testing.T, id int64, col string) string {
	t.Helper()
	var v *string
	if err := e.db.QueryRow(`SELECT `+col+`::text FROM milestones WHERE id = $1`, id).Scan(&v); err != nil {
		t.Fatalf("milestone %s: %v", col, err)
	}
	if v == nil {
		return ""
	}
	return *v
}

func (e metaEnv) milestoneCount(t *testing.T) int {
	t.Helper()
	return e.count(t, `SELECT COUNT(*) FROM milestones WHERE repo_id = $1`, e.repoID)
}

func TestMilestones_APIRead(t *testing.T) {
	e := newGitMetaEnv(t)

	rr := e.do(t, metaReq{method: "GET", target: e.path("/milestones")})
	wantStatus(t, rr, http.StatusOK)

	_, n1 := e.seedMilestone(t, "alpha")
	e.seedMilestone(t, "beta")

	rr = e.do(t, metaReq{method: "GET", target: e.path("/milestones")})
	wantStatus(t, rr, http.StatusOK)
	var list []struct{ Title string }
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil || len(list) != 2 {
		t.Fatalf("list = %s (%v)", rr.Body.String(), err)
	}

	rr = e.do(t, metaReq{method: "GET", target: e.path("/milestones/%d", n1)})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, `"alpha"`)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/milestones/x")}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/milestones/99")}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: "/api/repos/" + e.owner.name + "/nope/milestones"}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: "/api/repos/" + e.owner.name + "/nope/milestones/1"}), http.StatusNotFound)

	e.makePrivate(t)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/milestones/%d", n1), token: e.outsider.token}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/milestones"), token: e.outsider.token}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/milestones"), token: e.writer.token}), http.StatusOK)
}

func TestMilestones_CreateJSON(t *testing.T) {
	e := newGitMetaEnv(t)

	rr := e.do(t, metaReq{method: "POST", target: e.path("/milestones"), token: e.writer.token,
		json: `{"title":"v1","description":"first","due_date":"2030-05-06T00:00:00Z"}`})
	wantStatus(t, rr, http.StatusCreated)
	var got struct {
		ID     int64 `json:"id"`
		Number int   `json:"number"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil || got.Number != 1 {
		t.Fatalf("created = %s (%v)", rr.Body.String(), err)
	}
	if d := e.milestoneCol(t, got.ID, "due_date::date"); d != "2030-05-06" {
		t.Errorf("due_date = %q", d)
	}

	rr = e.do(t, metaReq{method: "POST", target: e.path("/milestones"), token: e.owner.token, json: `{"title":"v2","due_date":null}`})
	wantStatus(t, rr, http.StatusCreated)
	rr = e.do(t, metaReq{method: "POST", target: e.path("/milestones"), token: e.owner.token, json: `{"title":"v3","due_date":""}`})
	wantStatus(t, rr, http.StatusCreated)
	if e.milestoneCount(t) != 3 {
		t.Errorf("milestones = %d, want 3", e.milestoneCount(t))
	}
}

func TestMilestones_CreateForm(t *testing.T) {
	e := newGitMetaEnv(t)

	rr := e.do(t, metaReq{method: "POST", target: e.path("/milestones"), token: e.owner.token, htmx: true,
		form: url.Values{"title": {"form ms"}, "due_date": {"2031-01-02"}}})
	wantStatus(t, rr, http.StatusCreated)
	if n := e.count(t, `SELECT COUNT(*) FROM milestones WHERE repo_id = $1 AND due_date::date = '2031-01-02'`, e.repoID); n != 1 {
		t.Errorf("form due date not stored")
	}
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/milestones"), token: e.owner.token,
		form: url.Values{"title": {"no htmx header"}}}), http.StatusCreated)
}

func TestMilestones_CreateRefusals(t *testing.T) {
	e := newGitMetaEnv(t)
	cases := []struct {
		name string
		req  metaReq
		want int
	}{
		{"anonymous", metaReq{method: "POST", target: e.path("/milestones"), json: `{"title":"a"}`}, http.StatusUnauthorized},
		{"outsider", metaReq{method: "POST", target: e.path("/milestones"), token: e.outsider.token, json: `{"title":"a"}`}, http.StatusForbidden},
		{"unknown repo", metaReq{method: "POST", target: "/api/repos/" + e.owner.name + "/nope/milestones", token: e.owner.token, json: `{"title":"a"}`}, http.StatusNotFound},
		{"bad json", metaReq{method: "POST", target: e.path("/milestones"), token: e.owner.token, json: `{`}, http.StatusBadRequest},
		{"no title", metaReq{method: "POST", target: e.path("/milestones"), token: e.owner.token, json: `{"description":"x"}`}, http.StatusBadRequest},
		{"date-only in JSON", metaReq{method: "POST", target: e.path("/milestones"), token: e.owner.token, json: `{"title":"a","due_date":"2030-01-01"}`}, http.StatusBadRequest},
		{"form without title", metaReq{method: "POST", target: e.path("/milestones"), token: e.owner.token, htmx: true, form: url.Values{"title": {""}}}, http.StatusBadRequest},
		{"form bad date", metaReq{method: "POST", target: e.path("/milestones"), token: e.owner.token, htmx: true, form: url.Values{"title": {"a"}, "due_date": {"31/12/2030"}}}, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantStatus(t, e.do(t, c.req), c.want) })
	}
	if e.milestoneCount(t) != 0 {
		t.Errorf("refused requests created %d milestones", e.milestoneCount(t))
	}
}

func TestMilestones_Update(t *testing.T) {
	e := newGitMetaEnv(t)
	id, n := e.seedMilestone(t, "orig")
	target := e.path("/milestones/%d", n)

	rr := e.do(t, metaReq{method: "PATCH", target: target, token: e.writer.token, json: `{"title":"renamed","description":"d2","due_date":"2032-03-04"}`})
	wantStatus(t, rr, http.StatusOK)
	if e.milestoneCol(t, id, "title") != "renamed" || e.milestoneCol(t, id, "description") != "d2" || e.milestoneCol(t, id, "due_date::date") != "2032-03-04" {
		t.Error("milestone not updated")
	}

	rr = e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, json: `{"description":"only desc"}`})
	wantStatus(t, rr, http.StatusOK)
	if e.milestoneCol(t, id, "title") != "renamed" || e.milestoneCol(t, id, "description") != "only desc" {
		t.Error("an empty title must keep the existing title")
	}

	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, json: `{"state":"closed"}`}), http.StatusOK)
	if e.milestoneCol(t, id, "state") != "closed" || e.milestoneCol(t, id, "closed_at") == "" {
		t.Error("milestone not closed")
	}
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, htmx: true, form: url.Values{"state": {"open"}}}), http.StatusOK)
	if e.milestoneCol(t, id, "state") != "open" || e.milestoneCol(t, id, "closed_at") != "" {
		t.Error("milestone not reopened")
	}

	rr = e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, htmx: true, form: url.Values{"title": {"via form"}, "description": {"x"}, "due_date": {"2033-01-01"}}})
	wantStatus(t, rr, http.StatusOK)
	if e.milestoneCol(t, id, "title") != "via form" {
		t.Error("form update not stored")
	}

	cases := []struct {
		name string
		req  metaReq
		want int
	}{
		{"anonymous", metaReq{method: "PATCH", target: target, json: `{"title":"x"}`}, http.StatusUnauthorized},
		{"outsider", metaReq{method: "PATCH", target: target, token: e.outsider.token, json: `{"title":"x"}`}, http.StatusForbidden},
		{"bad number", metaReq{method: "PATCH", target: e.path("/milestones/x"), token: e.owner.token, json: `{"title":"x"}`}, http.StatusBadRequest},
		{"bad json", metaReq{method: "PATCH", target: target, token: e.owner.token, json: `{`}, http.StatusBadRequest},
		{"bad date", metaReq{method: "PATCH", target: target, token: e.owner.token, json: `{"title":"x","due_date":"soon"}`}, http.StatusBadRequest},
		{"missing milestone", metaReq{method: "PATCH", target: e.path("/milestones/99"), token: e.owner.token, json: `{"title":"x"}`}, http.StatusNotFound},
		{"missing milestone keeps title lookup", metaReq{method: "PATCH", target: e.path("/milestones/99"), token: e.owner.token, json: `{"description":"x"}`}, http.StatusNotFound},
		{"close missing", metaReq{method: "PATCH", target: e.path("/milestones/99"), token: e.owner.token, json: `{"state":"closed"}`}, http.StatusNotFound},
		{"reopen missing", metaReq{method: "PATCH", target: e.path("/milestones/99"), token: e.owner.token, json: `{"state":"open"}`}, http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rr := e.do(t, c.req)
			wantStatus(t, rr, c.want)
			if strings.HasPrefix(c.name, "missing") || strings.HasSuffix(c.name, "missing") {
				bodyHas(t, rr, "milestone not found")
			}
		})
	}
	if e.milestoneCol(t, id, "title") != "via form" {
		t.Error("refused updates changed the title")
	}
}

func TestMilestones_Delete(t *testing.T) {
	e := newGitMetaEnv(t)
	_, a := e.seedMilestone(t, "a")
	_, b := e.seedMilestone(t, "b")

	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/milestones/%d", a)}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/milestones/%d", a), token: e.outsider.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/milestones/x"), token: e.owner.token}), http.StatusBadRequest)
	if e.milestoneCount(t) != 2 {
		t.Fatalf("refusals deleted milestones: %d left", e.milestoneCount(t))
	}
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/milestones/%d", a), token: e.writer.token}), http.StatusNoContent)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/milestones/%d", b), token: e.owner.token}), http.StatusNoContent)
	if e.milestoneCount(t) != 0 {
		t.Errorf("milestones left = %d", e.milestoneCount(t))
	}
}

func TestMilestones_SidebarSetIssueAndPull(t *testing.T) {
	e := newGitMetaEnv(t)
	msID, _ := e.seedMilestone(t, "sprint")
	issueID, issueNum := e.seedIssue(t, "an issue", "open")
	pullID, pullNum := e.seedPull(t, "a pull", "open")
	form := func(id any) url.Values { return url.Values{"milestone_id": {fmt.Sprint(id)}} }
	issueURL := e.path("/issues/%d/milestone", issueNum)
	pullURL := e.path("/pulls/%d/milestone", pullNum)
	issueMS := func() string {
		var v *string
		if err := e.db.QueryRow(`SELECT milestone_id::text FROM issues WHERE id = $1`, issueID).Scan(&v); err != nil || v == nil {
			return ""
		}
		return *v
	}
	pullMS := func() string {
		var v *string
		if err := e.db.QueryRow(`SELECT milestone_id::text FROM pull_requests WHERE id = $1`, pullID).Scan(&v); err != nil || v == nil {
			return ""
		}
		return *v
	}

	rr := e.do(t, metaReq{method: "POST", target: issueURL, token: e.writer.token, form: form(msID)})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "sprint")
	if !strings.Contains(rr.Header().Get("HX-Trigger"), "Milestone set") || issueMS() != fmt.Sprint(msID) {
		t.Errorf("issue milestone = %q, trigger = %q", issueMS(), rr.Header().Get("HX-Trigger"))
	}
	rr = e.do(t, metaReq{method: "POST", target: issueURL, token: e.owner.token, form: url.Values{"milestone_id": {""}}})
	wantStatus(t, rr, http.StatusOK)
	if !strings.Contains(rr.Header().Get("HX-Trigger"), "Milestone cleared") || issueMS() != "" {
		t.Errorf("issue milestone after clear = %q", issueMS())
	}

	wantStatus(t, e.do(t, metaReq{method: "POST", target: pullURL, token: e.owner.token, form: form(msID)}), http.StatusOK)
	if pullMS() != fmt.Sprint(msID) {
		t.Errorf("pull milestone = %q", pullMS())
	}
	wantStatus(t, e.do(t, metaReq{method: "POST", target: pullURL, token: e.owner.token, form: url.Values{}}), http.StatusOK)
	if pullMS() != "" {
		t.Errorf("pull milestone after clear = %q", pullMS())
	}

	other := newGitMetaEnv(t)
	foreignID, _ := other.seedMilestone(t, "foreign")
	for _, target := range []string{issueURL, pullURL} {
		rr = e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, form: form(foreignID)})
		wantStatus(t, rr, http.StatusUnprocessableEntity)
		if !strings.Contains(rr.Header().Get("HX-Trigger"), "different repository") {
			t.Errorf("cross-repo trigger = %q", rr.Header().Get("HX-Trigger"))
		}
		rr = e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, form: form(999999999)})
		wantStatus(t, rr, http.StatusUnprocessableEntity)
		if !strings.Contains(rr.Header().Get("HX-Trigger"), "no longer exists") {
			t.Errorf("missing milestone trigger = %q", rr.Header().Get("HX-Trigger"))
		}
		wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, form: form("abc")}), http.StatusBadRequest)
		wantStatus(t, e.do(t, metaReq{method: "POST", target: target, form: form(msID)}), http.StatusUnauthorized)
		wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.outsider.token, form: form(msID)}), http.StatusForbidden)
	}
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/issues/x/milestone"), token: e.owner.token, form: form(msID)}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/pulls/x/milestone"), token: e.owner.token, form: form(msID)}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/issues/99/milestone"), token: e.owner.token, form: form(msID)}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/pulls/99/milestone"), token: e.owner.token, form: form(msID)}), http.StatusNotFound)
	if issueMS() != "" || pullMS() != "" {
		t.Error("refused requests attached a milestone")
	}
}

func TestMilestones_ListPage(t *testing.T) {
	e := newGitMetaEnv(t)
	e.seedMilestone(t, "open-ms")
	closedID, _ := e.seedMilestone(t, "closed-ms")
	closeMilestone(t, e, closedID)

	rr := e.page(t, e.pagePath("/milestones"), "", false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "open-ms")
	bodyHas(t, rr, "closed-ms")
	if strings.Contains(rr.Body.String(), "/milestones/new") {
		t.Error("anonymous viewers must not see the new-milestone link")
	}
	rr = e.page(t, e.pagePath("/milestones"), e.writer.token, false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "/milestones/new")

	wantStatus(t, e.page(t, "/"+e.owner.name+"/nope/milestones", "", false), http.StatusNotFound)
	e.makePrivate(t)
	wantStatus(t, e.page(t, e.pagePath("/milestones"), e.outsider.token, false), http.StatusNotFound)
}

func closeMilestone(t *testing.T, e metaEnv, id int64) {
	t.Helper()
	if _, err := e.db.Exec(`UPDATE milestones SET state = 'closed', closed_at = NOW() WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
}

func TestMilestones_NewPageAndSubmit(t *testing.T) {
	e := newGitMetaEnv(t)
	newPage := e.pagePath("/milestones/new")

	rr := e.page(t, newPage, "", false)
	wantStatus(t, rr, http.StatusSeeOther)
	if !strings.HasPrefix(rr.Header().Get("Location"), "/login") {
		t.Errorf("anonymous Location = %q", rr.Header().Get("Location"))
	}
	wantStatus(t, e.page(t, newPage, e.outsider.token, false), http.StatusForbidden)
	wantStatus(t, e.page(t, "/"+e.owner.name+"/nope/milestones/new", e.owner.token, false), http.StatusNotFound)
	wantStatus(t, e.page(t, newPage, e.writer.token, false), http.StatusOK)

	post := func(token string, form url.Values) *httpResult {
		rr := e.do(t, metaReq{method: "POST", target: newPage, token: token, form: form})
		return &httpResult{rr.Code, rr.Header().Get("Location"), rr.Body.String()}
	}
	if r := post("", url.Values{"title": {"x"}}); r.code != http.StatusSeeOther || !strings.HasPrefix(r.location, "/login") {
		t.Errorf("anonymous submit = %+v", r)
	}
	if r := post(e.outsider.token, url.Values{"title": {"x"}}); r.code != http.StatusForbidden {
		t.Errorf("outsider submit = %d", r.code)
	}
	if r := post(e.owner.token, url.Values{"title": {"t"}, "due_date": {"2030-13-45"}}); r.code != http.StatusOK {
		t.Errorf("impossible date status = %d, want the form re-rendered", r.code)
	}
	if r := post(e.owner.token, url.Values{"title": {"   "}}); !strings.Contains(r.body, "Title is required") {
		t.Errorf("blank title body lacks the error: %.200s", r.body)
	}
	if r := post(e.owner.token, url.Values{"title": {"t"}, "due_date": {"nope"}}); !strings.Contains(r.body, "valid date") {
		t.Errorf("bad date body lacks the error: %.200s", r.body)
	}
	if e.milestoneCount(t) != 0 {
		t.Fatalf("invalid submissions created %d milestones", e.milestoneCount(t))
	}

	r := post(e.writer.token, url.Values{"title": {"  Real  "}, "description": {"d"}, "due_date": {"2034-05-06"}})
	if r.code != http.StatusSeeOther || r.location != "/"+e.owner.name+"/"+e.repoName+"/milestones" {
		t.Fatalf("submit = %+v", r)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM milestones WHERE repo_id = $1 AND title = 'Real' AND due_date::date = '2034-05-06'`, e.repoID); n != 1 {
		t.Error("milestone not stored with trimmed title and due date")
	}
}

type httpResult struct {
	code     int
	location string
	body     string
}

func TestMilestones_DetailPage(t *testing.T) {
	e := newGitMetaEnv(t)
	msID, n := e.seedMilestone(t, "sprint 1")
	for i := 0; i < 12; i++ {
		id, _ := e.seedIssue(t, fmt.Sprintf("open issue %02d", i), "open")
		e.attachIssue(t, id, msID)
	}
	closedIssue, _ := e.seedIssue(t, "done issue", "closed")
	e.attachIssue(t, closedIssue, msID)
	pullID, _ := e.seedPull(t, "milestone pull", "open")
	if _, err := e.db.Exec(`UPDATE pull_requests SET milestone_id = $1 WHERE id = $2`, msID, pullID); err != nil {
		t.Fatal(err)
	}
	detail := e.pagePath("/milestones/%d", n)

	rr := e.page(t, detail, "", false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "sprint 1")
	bodyHas(t, rr, "desc of sprint 1")
	bodyHas(t, rr, "open issue")

	p1 := e.page(t, detail, "", false).Body.String()
	p2 := e.page(t, detail+"?page=2", "", false).Body.String()
	if p1 == p2 {
		t.Error("page 2 must differ from page 1")
	}
	wantStatus(t, e.page(t, detail+"?page=99", "", false), http.StatusOK)
	wantStatus(t, e.page(t, detail+"?page=-3", "", false), http.StatusOK)

	rr = e.page(t, detail+"?state=closed", "", false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "done issue")
	rr = e.page(t, detail+"?tab=pulls", e.owner.token, false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "milestone pull")
	wantStatus(t, e.page(t, detail+"?tab=pulls&state=closed", "", false), http.StatusOK)
	wantStatus(t, e.page(t, detail+"?tab=bogus&state=bogus", "", false), http.StatusOK)

	wantStatus(t, e.page(t, e.pagePath("/milestones/99"), "", false), http.StatusNotFound)
	wantStatus(t, e.page(t, e.pagePath("/milestones/abc"), "", false), http.StatusNotFound)
	wantStatus(t, e.page(t, "/"+e.owner.name+"/nope/milestones/1", "", false), http.StatusNotFound)
	e.makePrivate(t)
	wantStatus(t, e.page(t, detail, e.outsider.token, false), http.StatusNotFound)
}

func (e metaEnv) attachIssue(t *testing.T, issueID, milestoneID int64) {
	t.Helper()
	if _, err := e.db.Exec(`UPDATE issues SET milestone_id = $1 WHERE id = $2`, milestoneID, issueID); err != nil {
		t.Fatal(err)
	}
}

func TestMilestones_DetailActions(t *testing.T) {
	e := newGitMetaEnv(t)
	id, n := e.seedMilestone(t, "act")
	detail := e.pagePath("/milestones/%d", n)
	act := func(token, action string) *httpResult {
		rr := e.do(t, metaReq{method: "POST", target: detail, token: token, form: url.Values{"action": {action}}})
		return &httpResult{rr.Code, rr.Header().Get("Location"), rr.Body.String()}
	}

	if r := act("", "close"); r.code != http.StatusSeeOther || !strings.HasPrefix(r.location, "/login") {
		t.Errorf("anonymous = %+v", r)
	}
	if r := act(e.outsider.token, "close"); r.code != http.StatusForbidden {
		t.Errorf("outsider = %d", r.code)
	}
	if e.milestoneCol(t, id, "state") != "open" {
		t.Fatal("refusals closed the milestone")
	}
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.pagePath("/milestones/abc"), token: e.owner.token, form: url.Values{"action": {"close"}}}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: "/" + e.owner.name + "/nope/milestones/1", token: e.owner.token, form: url.Values{"action": {"close"}}}), http.StatusNotFound)

	if r := act(e.writer.token, "close"); r.code != http.StatusSeeOther || r.location != "/"+e.owner.name+"/"+e.repoName+"/milestones/"+fmt.Sprint(n) {
		t.Errorf("close = %+v", r)
	}
	if e.milestoneCol(t, id, "state") != "closed" {
		t.Error("milestone not closed")
	}
	if r := act(e.owner.token, "reopen"); r.code != http.StatusSeeOther {
		t.Errorf("reopen = %+v", r)
	}
	if e.milestoneCol(t, id, "state") != "open" {
		t.Error("milestone not reopened")
	}
	if r := act(e.owner.token, "whatever"); r.code != http.StatusSeeOther || !strings.HasSuffix(r.location, fmt.Sprintf("/milestones/%d", n)) {
		t.Errorf("unknown action = %+v", r)
	}
	if r := act(e.owner.token, "delete"); r.code != http.StatusSeeOther || !strings.HasSuffix(r.location, "/milestones") {
		t.Errorf("delete = %+v", r)
	}
	if e.milestoneCount(t) != 0 {
		t.Error("milestone not deleted")
	}
	if r := act(e.owner.token, "close"); r.code != http.StatusNotFound || !strings.Contains(r.body, "milestone not found") {
		t.Errorf("close of a missing milestone = %d %s, want 404", r.code, r.body)
	}
	if r := act(e.owner.token, "reopen"); r.code != http.StatusNotFound || !strings.Contains(r.body, "milestone not found") {
		t.Errorf("reopen of a missing milestone = %d %s, want 404", r.code, r.body)
	}
}

func TestMilestones_Fragments(t *testing.T) {
	e := newGitMetaEnv(t)
	id, n := e.seedMilestone(t, "frag")
	base := fmt.Sprintf("/milestones/%d", n)
	frag := func(suffix string) string { return e.path("%s", base+suffix) }

	for _, section := range []string{"title", "body", "due"} {
		rr := e.do(t, metaReq{method: "GET", target: frag("/" + section)})
		wantStatus(t, rr, http.StatusOK)
		rr = e.do(t, metaReq{method: "GET", target: frag("/" + section + "?mode=edit"), token: e.owner.token})
		wantStatus(t, rr, http.StatusOK)
		wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/milestones/x/%s", section)}), http.StatusBadRequest)
		wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/milestones/99/%s", section)}), http.StatusNotFound)
	}
	rr := e.do(t, metaReq{method: "GET", target: frag("/title?mode=edit"), token: e.owner.token})
	bodyHas(t, rr, `name="title"`)
	rr = e.do(t, metaReq{method: "GET", target: frag("/body")})
	bodyHas(t, rr, "desc of frag")

	rr = e.do(t, metaReq{method: "PATCH", target: frag("/title"), token: e.writer.token, form: url.Values{"title": {"  new title "}}})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "new title")
	if e.milestoneCol(t, id, "title") != "new title" {
		t.Errorf("title = %q", e.milestoneCol(t, id, "title"))
	}
	rr = e.do(t, metaReq{method: "PATCH", target: frag("/body"), token: e.owner.token, form: url.Values{"description": {"**bold** text"}}})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "<strong>bold</strong>")
	if e.milestoneCol(t, id, "description") != "**bold** text" || e.milestoneCol(t, id, "title") != "new title" {
		t.Error("body edit must only change the description")
	}
	rr = e.do(t, metaReq{method: "PATCH", target: frag("/due"), token: e.owner.token, form: url.Values{"due_date": {"2035-07-08"}}})
	wantStatus(t, rr, http.StatusOK)
	if e.milestoneCol(t, id, "due_date::date") != "2035-07-08" || e.milestoneCol(t, id, "description") != "**bold** text" {
		t.Error("due edit must only change the due date")
	}
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: frag("/due"), token: e.owner.token, form: url.Values{"due_date": {""}}}), http.StatusOK)
	if e.milestoneCol(t, id, "due_date") != "" {
		t.Error("a blank due date must clear it")
	}

	badWrites := []struct {
		name, section string
		form          url.Values
		want          int
	}{
		{"blank title", "title", url.Values{"title": {"  "}}, http.StatusBadRequest},
		{"bad due date", "due", url.Values{"due_date": {"someday"}}, http.StatusBadRequest},
	}
	for _, c := range badWrites {
		wantStatus(t, e.do(t, metaReq{method: "PATCH", target: frag("/" + c.section), token: e.owner.token, form: c.form}), c.want)
	}
	for _, section := range []string{"title", "body", "due"} {
		wantStatus(t, e.do(t, metaReq{method: "PATCH", target: frag("/" + section)}), http.StatusUnauthorized)
		wantStatus(t, e.do(t, metaReq{method: "PATCH", target: frag("/" + section), token: e.outsider.token}), http.StatusForbidden)
		wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/milestones/x/%s", section), token: e.owner.token}), http.StatusBadRequest)
		wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/milestones/99/%s", section), token: e.owner.token}), http.StatusNotFound)
	}
	if e.milestoneCol(t, id, "title") != "new title" {
		t.Error("refusals changed the title")
	}

	e.makePrivate(t)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: frag("/title"), token: e.outsider.token}), http.StatusNotFound)
}
