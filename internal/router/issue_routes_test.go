package router_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func (e metaEnv) issueCol(t *testing.T, id int64, col string) string {
	t.Helper()
	var v *string
	if err := e.db.QueryRow(`SELECT `+col+`::text FROM issues WHERE id = $1`, id).Scan(&v); err != nil {
		t.Fatalf("issue %s: %v", col, err)
	}
	if v == nil {
		return ""
	}
	return *v
}

func (e metaEnv) issueCount(t *testing.T) int {
	t.Helper()
	return e.count(t, `SELECT COUNT(*) FROM issues WHERE repo_id = $1`, e.repoID)
}

func TestIssues_ListAndGet(t *testing.T) {
	e := newGitMetaEnv(t)
	rr := e.do(t, metaReq{method: "GET", target: e.path("/issues")})
	wantStatus(t, rr, http.StatusOK)

	_, n1 := e.seedIssue(t, "public one", "open")
	privID, privN := e.seedIssue(t, "secret one", "open")
	testutil.Exec(t, e.db, `UPDATE issues SET visibility = 'private' WHERE id = $1`, privID)

	titles := func(token string) []string {
		rr := e.do(t, metaReq{method: "GET", target: e.path("/issues"), token: token})
		wantStatus(t, rr, http.StatusOK)
		var list []struct{ Title string }
		if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
			t.Fatalf("decode %s: %v", rr.Body.String(), err)
		}
		var out []string
		for _, i := range list {
			out = append(out, i.Title)
		}
		return out
	}
	if got := titles(""); len(got) != 1 || got[0] != "public one" {
		t.Errorf("anonymous sees %v, want only the public issue", got)
	}
	if got := titles(e.outsider.token); len(got) != 1 {
		t.Errorf("outsider sees %v", got)
	}
	if got := titles(e.writer.token); len(got) != 2 {
		t.Errorf("writer sees %v, want both issues", got)
	}

	rr = e.do(t, metaReq{method: "GET", target: e.path("/issues/%d", n1)})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "public one")
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/issues/%d", privN)}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/issues/%d", privN), token: e.outsider.token}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/issues/%d", privN), token: e.writer.token}), http.StatusOK)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/issues/x")}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/issues/99")}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: "/api/repos/" + e.owner.name + "/nope/issues"}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: "/api/repos/" + e.owner.name + "/nope/issues/1"}), http.StatusNotFound)
}

func TestIssues_Create(t *testing.T) {
	e := newGitMetaEnv(t)

	rr := e.do(t, metaReq{method: "POST", target: e.path("/issues"), token: e.outsider.token, json: `{"title":"from outsider","body":"b"}`})
	wantStatus(t, rr, http.StatusCreated)
	var got struct {
		ID         int64  `json:"id"`
		Number     int    `json:"number"`
		Visibility string `json:"visibility"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil || got.Number != 1 || got.Visibility != "public" {
		t.Fatalf("created = %s (%v)", rr.Body.String(), err)
	}
	if e.issueCol(t, got.ID, "author_id") != fmt.Sprint(e.outsider.id) || e.issueCol(t, got.ID, "state") != "open" {
		t.Error("issue not stored as an open issue by the caller")
	}

	rr = e.do(t, metaReq{method: "POST", target: e.path("/issues"), token: e.writer.token, json: `{"title":"hidden","visibility":"private"}`})
	wantStatus(t, rr, http.StatusCreated)
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil || got.Visibility != "private" {
		t.Errorf("writer's private issue = %s", rr.Body.String())
	}
	rr = e.do(t, metaReq{method: "POST", target: e.path("/issues"), token: e.owner.token, json: `{"title":"odd","visibility":"whatever"}`})
	wantStatus(t, rr, http.StatusCreated)
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil || got.Visibility != "public" {
		t.Errorf("unknown visibility must fall back to public: %s", rr.Body.String())
	}
	if e.issueCount(t) != 3 {
		t.Errorf("issues = %d, want 3", e.issueCount(t))
	}
}

func TestIssues_CreateRefusals(t *testing.T) {
	e := newGitMetaEnv(t)
	cases := []struct {
		name string
		req  metaReq
		want int
	}{
		{"anonymous", metaReq{method: "POST", target: e.path("/issues"), json: `{"title":"x"}`}, http.StatusUnauthorized},
		{"unknown repo", metaReq{method: "POST", target: "/api/repos/" + e.owner.name + "/nope/issues", token: e.owner.token, json: `{"title":"x"}`}, http.StatusNotFound},
		{"bad json", metaReq{method: "POST", target: e.path("/issues"), token: e.owner.token, json: `{`}, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantStatus(t, e.do(t, c.req), c.want) })
	}
	if e.issueCount(t) != 0 {
		t.Fatalf("refused requests created %d issues", e.issueCount(t))
	}

	testutil.Exec(t, e.db, `UPDATE repositories SET allow_issues = FALSE WHERE id = $1`, e.repoID)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/issues"), token: e.owner.token, json: `{"title":"x"}`}), http.StatusNotFound)
	if e.issueCount(t) != 0 {
		t.Error("an issue was created while issues are disabled")
	}

	e2 := newGitMetaEnv(t)
	e2.makePrivate(t)
	wantStatus(t, e2.do(t, metaReq{method: "POST", target: e2.path("/issues"), token: e2.outsider.token, json: `{"title":"x"}`}), http.StatusNotFound)
}

func TestIssues_Update(t *testing.T) {
	e := newGitMetaEnv(t)
	id, n := e.seedIssue(t, "to close", "open")
	target := e.path("/issues/%d", n)

	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, json: `{"state":"closed"}`}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.outsider.token, json: `{"state":"closed"}`}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/issues/x"), token: e.owner.token, json: `{"state":"closed"}`}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, json: `{`}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, json: `{"state":"merged"}`}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, json: `{}`}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/issues/99"), token: e.owner.token, json: `{"state":"closed"}`}), http.StatusInternalServerError)
	if e.issueCol(t, id, "state") != "open" {
		t.Fatal("refusals closed the issue")
	}

	rr := e.do(t, metaReq{method: "PATCH", target: target, token: e.writer.token, json: `{"state":"closed"}`})
	wantStatus(t, rr, http.StatusOK)
	if e.issueCol(t, id, "state") != "closed" || e.issueCol(t, id, "closed_at") == "" {
		t.Error("issue not closed")
	}
	rr = e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, htmx: true, form: url.Values{"state": {"open"}}})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "to close")
	if e.issueCol(t, id, "state") != "open" {
		t.Error("issue not reopened")
	}
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, htmx: true, form: url.Values{"state": {"bogus"}}}), http.StatusBadRequest)
}

func TestIssues_PinAndLock(t *testing.T) {
	e := newGitMetaEnv(t)
	id, n := e.seedIssue(t, "maintained", "open")

	for _, c := range []struct{ route, on, off, col string }{
		{"pin", "pin", "unpin", "is_pinned"},
		{"lock", "lock", "unlock", "is_locked"},
	} {
		t.Run(c.route, func(t *testing.T) {
			target := e.path("/issues/%d/%s", n, c.route)
			wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, json: `{"action":"` + c.on + `"}`}), http.StatusUnauthorized)
			wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.writer.token, json: `{"action":"` + c.on + `"}`}), http.StatusForbidden)
			wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.outsider.token, json: `{"action":"` + c.on + `"}`}), http.StatusForbidden)
			wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/issues/x/%s", c.route), token: e.owner.token, json: `{}`}), http.StatusBadRequest)
			wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, json: `{`}), http.StatusBadRequest)
			wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/issues/99/%s", c.route), token: e.owner.token, json: `{"action":"` + c.on + `"}`}), http.StatusInternalServerError)
			if e.issueCol(t, id, c.col) != "false" {
				t.Fatalf("refusals set %s", c.col)
			}

			wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, json: `{"action":"` + c.on + `"}`}), http.StatusNoContent)
			if e.issueCol(t, id, c.col) != "true" {
				t.Errorf("%s not set", c.col)
			}
			wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, json: `{"action":"` + c.off + `"}`}), http.StatusNoContent)
			if e.issueCol(t, id, c.col) != "false" {
				t.Errorf("%s not cleared", c.col)
			}

			rr := e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, htmx: true, form: url.Values{"action": {c.on}}})
			wantStatus(t, rr, http.StatusOK)
			if rr.Header().Get("HX-Trigger") == "" || e.issueCol(t, id, c.col) != "true" {
				t.Errorf("HTMX %s: trigger %q, %s=%s", c.on, rr.Header().Get("HX-Trigger"), c.col, e.issueCol(t, id, c.col))
			}
			rr = e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, htmx: true, form: url.Values{"action": {c.off}}})
			wantStatus(t, rr, http.StatusOK)
			if e.issueCol(t, id, c.col) != "false" {
				t.Errorf("HTMX %s did not clear %s", c.off, c.col)
			}
		})
	}
}

func TestIssues_PinLimit(t *testing.T) {
	e := newGitMetaEnv(t)
	pinned := 0
	for i := 0; i < 8; i++ {
		_, n := e.seedIssue(t, "pin me", "open")
		rr := e.do(t, metaReq{method: "PATCH", target: e.path("/issues/%d/pin", n), token: e.owner.token, json: `{"action":"pin"}`})
		if rr.Code == http.StatusNoContent {
			pinned++
		}
	}
	if pinned == 0 || pinned == 8 {
		t.Fatalf("pinned %d of 8; the pin limit should cap it", pinned)
	}
	if got := e.count(t, `SELECT COUNT(*) FROM issues WHERE repo_id = $1 AND is_pinned`, e.repoID); got != pinned {
		t.Errorf("stored pinned = %d, want %d", got, pinned)
	}
}

func TestIssues_Pages(t *testing.T) {
	e := newGitMetaEnv(t)
	_, n := e.seedIssue(t, "visible issue", "open")
	e.seedIssue(t, "finished issue", "closed")
	for i := 0; i < 3; i++ {
		e.seedIssue(t, "filler issue", "open")
	}

	rr := e.page(t, e.pagePath("/issues"), "", false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "visible issue")
	rr = e.page(t, e.pagePath("/issues?state=closed"), "", false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "finished issue")
	rr = e.page(t, e.pagePath("/issues?q=visible"), e.owner.token, false)
	wantStatus(t, rr, http.StatusOK)
	wantStatus(t, e.page(t, e.pagePath("/issues?page=2&sort=oldest"), "", false), http.StatusOK)
	wantStatus(t, e.page(t, e.pagePath("/issues"), "", true), http.StatusOK)

	rr = e.page(t, e.pagePath("/issues/%d", n), "", false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "visible issue")
	rr = e.page(t, e.pagePath("/issues/%d", n), e.owner.token, false)
	wantStatus(t, rr, http.StatusOK)
	wantStatus(t, e.page(t, e.pagePath("/issues/999"), "", false), http.StatusNotFound)
	wantStatus(t, e.page(t, "/"+e.owner.name+"/nope/issues", "", false), http.StatusNotFound)

	e.makePrivate(t)
	wantStatus(t, e.page(t, e.pagePath("/issues"), e.outsider.token, false), http.StatusNotFound)
	wantStatus(t, e.page(t, e.pagePath("/issues/%d", n), e.outsider.token, false), http.StatusNotFound)
}

func TestIssues_NewPageAndSubmit(t *testing.T) {
	e := newGitMetaEnv(t)
	newPage := e.pagePath("/issues/new")

	rr := e.page(t, newPage, "", false)
	if rr.Code != http.StatusSeeOther && rr.Code != http.StatusUnauthorized {
		t.Errorf("anonymous new page = %d", rr.Code)
	}
	wantStatus(t, e.page(t, newPage, e.outsider.token, false), http.StatusOK)
	wantStatus(t, e.page(t, newPage, e.writer.token, false), http.StatusOK)
	wantStatus(t, e.page(t, "/"+e.owner.name+"/nope/issues/new", e.owner.token, false), http.StatusNotFound)

	submit := func(token string, form url.Values) *httpResult {
		rr := e.do(t, metaReq{method: "POST", target: newPage, token: token, form: form})
		return &httpResult{rr.Code, rr.Header().Get("Location"), rr.Body.String()}
	}
	if r := submit("", url.Values{"title": {"x"}}); r.code != http.StatusSeeOther && r.code != http.StatusUnauthorized {
		t.Errorf("anonymous submit = %+v", r)
	}
	if r := submit(e.owner.token, url.Values{"title": {""}}); r.code != http.StatusOK || !strings.Contains(r.body, "required") {
		t.Errorf("blank title = %d %.200s", r.code, r.body)
	}
	if r := submit(e.owner.token, url.Values{"title": {strings.Repeat("t", 1000)}}); !strings.Contains(r.body, "too long") {
		t.Errorf("long title = %d %.200s", r.code, r.body)
	}
	if e.issueCount(t) != 0 {
		t.Fatalf("invalid submissions created %d issues", e.issueCount(t))
	}

	r := submit(e.outsider.token, url.Values{"title": {"Real issue"}, "body": {"details"}})
	if r.code != http.StatusSeeOther || r.location != "/"+e.owner.name+"/"+e.repoName+"/issues/1" {
		t.Fatalf("submit = %+v", r)
	}
	if e.issueCount(t) != 1 {
		t.Errorf("issues = %d, want 1", e.issueCount(t))
	}
	if r := submit(e.outsider.token, url.Values{"title": {"Sneaky"}, "visibility": {"private"}}); r.code == http.StatusSeeOther {
		t.Errorf("outsider created a private issue: %+v", r)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM issues WHERE repo_id = $1 AND visibility = 'private'`, e.repoID); n != 0 {
		t.Errorf("private issues = %d, want 0", n)
	}
}
