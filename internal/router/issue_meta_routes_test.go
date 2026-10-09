package router_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func (e metaEnv) issueField(t *testing.T, column string) string {
	t.Helper()
	var v *string
	if err := e.db.QueryRow(`SELECT `+column+` FROM issues WHERE id = $1`, e.issueID).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v == nil {
		return ""
	}
	return *v
}

func TestIssuePriority(t *testing.T) {
	e := newMetaEnv(t)
	target := e.path("/issues/1/priority")
	set := func(token, value string, htmx bool) *http.Response {
		return e.do(t, metaReq{method: "POST", target: target, token: token, form: url.Values{"priority": {value}}, htmx: htmx}).Result()
	}

	if rr := e.do(t, metaReq{method: "POST", target: target, form: url.Values{"priority": {"P1"}}}); rr.Code != http.StatusUnauthorized {
		t.Errorf("signed out: got %d, want 401", rr.Code)
	}
	if resp := set(e.outsider.token, "P1", false); resp.StatusCode != http.StatusForbidden {
		t.Errorf("outsider: got %d, want 403", resp.StatusCode)
	}
	if resp := set(e.owner.token, "P9", false); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown priority: got %d, want 400", resp.StatusCode)
	}
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/issues/x/priority"), token: e.owner.token}), http.StatusBadRequest)
	if got := e.issueField(t, "priority"); got != "" {
		t.Fatalf("refused requests set priority %q", got)
	}

	if resp := set(e.writer.token, "P2", false); resp.StatusCode != http.StatusOK {
		t.Fatalf("JSON set: got %d, want 200", resp.StatusCode)
	}
	if got := e.issueField(t, "priority"); got != "P2" {
		t.Errorf("priority = %q, want P2", got)
	}

	rr := e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, form: url.Values{"priority": {"P0"}}, htmx: true})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "P0")
	if !strings.Contains(rr.Header().Get("HX-Trigger"), "Priority set to P0") {
		t.Errorf("HX-Trigger = %q, want the set toast", rr.Header().Get("HX-Trigger"))
	}

	rr = e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, form: url.Values{"priority": {"none"}}, htmx: true})
	wantStatus(t, rr, http.StatusOK)
	if !strings.Contains(rr.Header().Get("HX-Trigger"), "Priority cleared") {
		t.Errorf("HX-Trigger = %q, want the cleared toast", rr.Header().Get("HX-Trigger"))
	}
	if got := e.issueField(t, "priority"); got != "" {
		t.Errorf("priority = %q after clearing", got)
	}
}

func TestIssueTitle(t *testing.T) {
	e := newMetaEnv(t)
	target := e.path("/issues/1/title")

	rr := e.do(t, metaReq{method: "GET", target: target})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "issue")
	wantStatus(t, e.do(t, metaReq{method: "GET", target: target + "?mode=edit", token: e.owner.token}), http.StatusOK)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/issues/x/title")}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/issues/99/title")}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: "/api/repos/" + e.owner.name + "/nope/issues/1/title"}), http.StatusNotFound)

	patch := func(token, title string) *httptest.ResponseRecorder {
		return e.do(t, metaReq{method: "PATCH", target: target, token: token, form: url.Values{"title": {title}}})
	}
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, form: url.Values{"title": {"x"}}}), http.StatusUnauthorized)
	wantStatus(t, patch(e.outsider.token, "hijack"), http.StatusForbidden)
	wantStatus(t, patch(e.owner.token, "   "), http.StatusBadRequest)
	wantStatus(t, patch(e.owner.token, strings.Repeat("t", 5000)), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/issues/x/title"), token: e.owner.token, form: url.Values{"title": {"x"}}}), http.StatusBadRequest)
	if got := e.issueField(t, "title"); got != "issue" {
		t.Fatalf("refused requests changed the title to %q", got)
	}

	rr = patch(e.writer.token, "  renamed  ")
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "renamed")
	if got := e.issueField(t, "title"); got != "renamed" {
		t.Errorf("title = %q, want it trimmed and stored", got)
	}
}

func TestIssueBody(t *testing.T) {
	e := newMetaEnv(t)
	target := e.path("/issues/1/body")

	wantStatus(t, e.do(t, metaReq{method: "GET", target: target}), http.StatusOK)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: target + "?mode=edit", token: e.writer.token}), http.StatusOK)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/issues/x/body")}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/issues/99/body")}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: "/api/repos/" + e.owner.name + "/nope/issues/1/body"}), http.StatusNotFound)

	form := url.Values{"body": {"**bold** text"}}
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, form: form}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.outsider.token, form: form}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/issues/x/body"), token: e.owner.token, form: form}), http.StatusBadRequest)
	if got := e.issueField(t, "body"); got != "" {
		t.Fatalf("refused requests set the body to %q", got)
	}

	rr := e.do(t, metaReq{method: "PATCH", target: target, token: e.writer.token, form: form})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "<strong>bold</strong>")
	if got := e.issueField(t, "body"); got != "**bold** text" {
		t.Errorf("body = %q", got)
	}
	rr = e.do(t, metaReq{method: "GET", target: target})
	bodyHas(t, rr, "<strong>bold</strong>")
}

func TestIssuePullLinks(t *testing.T) {
	e := newMetaEnv(t)
	issueSide := e.path("/issues/1/linked-pulls/1")
	pullSide := e.path("/pulls/1/linked-issues/1")
	links := func() int {
		return e.count(t, `SELECT COUNT(*) FROM pull_issue_links WHERE pull_id = $1 AND issue_id = $2`, e.pullID, e.issueID)
	}

	for _, m := range []string{"POST", "DELETE"} {
		wantStatus(t, e.do(t, metaReq{method: m, target: issueSide}), http.StatusUnauthorized)
		wantStatus(t, e.do(t, metaReq{method: m, target: pullSide}), http.StatusUnauthorized)
		wantStatus(t, e.do(t, metaReq{method: m, target: issueSide, token: e.outsider.token}), http.StatusForbidden)
		wantStatus(t, e.do(t, metaReq{method: m, target: pullSide, token: e.outsider.token}), http.StatusForbidden)
		wantStatus(t, e.do(t, metaReq{method: m, target: e.path("/issues/x/linked-pulls/1"), token: e.owner.token}), http.StatusBadRequest)
		wantStatus(t, e.do(t, metaReq{method: m, target: e.path("/issues/1/linked-pulls/x"), token: e.owner.token}), http.StatusBadRequest)
		wantStatus(t, e.do(t, metaReq{method: m, target: e.path("/issues/1/linked-pulls/99"), token: e.owner.token}), http.StatusNotFound)
		wantStatus(t, e.do(t, metaReq{method: m, target: e.path("/issues/99/linked-pulls/1"), token: e.owner.token}), http.StatusNotFound)
		wantStatus(t, e.do(t, metaReq{method: m, target: e.path("/pulls/x/linked-issues/1"), token: e.owner.token}), http.StatusBadRequest)
		wantStatus(t, e.do(t, metaReq{method: m, target: e.path("/pulls/1/linked-issues/x"), token: e.owner.token}), http.StatusBadRequest)
		wantStatus(t, e.do(t, metaReq{method: m, target: e.path("/pulls/99/linked-issues/1"), token: e.owner.token}), http.StatusNotFound)
		wantStatus(t, e.do(t, metaReq{method: m, target: e.path("/pulls/1/linked-issues/99"), token: e.owner.token}), http.StatusNotFound)
	}
	if links() != 0 {
		t.Fatal("refused requests created a link")
	}

	wantStatus(t, e.do(t, metaReq{method: "POST", target: issueSide, token: e.writer.token}), http.StatusNoContent)
	if links() != 1 {
		t.Fatal("issue-side link not stored")
	}
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: issueSide, token: e.writer.token}), http.StatusNoContent)
	if links() != 0 {
		t.Fatal("issue-side unlink left the link")
	}

	wantStatus(t, e.do(t, metaReq{method: "POST", target: pullSide, token: e.writer.token}), http.StatusNoContent)
	if links() != 1 {
		t.Fatal("pull-side link not stored")
	}
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: pullSide, token: e.writer.token}), http.StatusNoContent)
	if links() != 0 {
		t.Fatal("pull-side unlink left the link")
	}
}

func TestIssuePullLinks_HTMXRendersSidebarWithToast(t *testing.T) {
	e := newMetaEnv(t)
	cases := []struct {
		name, target, link, unlink string
	}{
		{"issue side", e.path("/issues/1/linked-pulls/1"), "Pull request linked", "Pull request unlinked"},
		{"pull side", e.path("/pulls/1/linked-issues/1"), "Issue linked", "Issue unlinked"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := e.do(t, metaReq{method: "POST", target: tc.target, token: e.owner.token, htmx: true})
			wantStatus(t, rr, http.StatusOK)
			if !strings.Contains(rr.Header().Get("HX-Trigger"), tc.link) {
				t.Errorf("HX-Trigger = %q, want %q", rr.Header().Get("HX-Trigger"), tc.link)
			}
			if n := e.count(t, `SELECT COUNT(*) FROM pull_issue_links WHERE pull_id = $1`, e.pullID); n != 1 {
				t.Errorf("links = %d, want 1", n)
			}
			rr = e.do(t, metaReq{method: "DELETE", target: tc.target, token: e.owner.token, htmx: true})
			wantStatus(t, rr, http.StatusOK)
			if !strings.Contains(rr.Header().Get("HX-Trigger"), tc.unlink) {
				t.Errorf("HX-Trigger = %q, want %q", rr.Header().Get("HX-Trigger"), tc.unlink)
			}
			if n := e.count(t, `SELECT COUNT(*) FROM pull_issue_links WHERE pull_id = $1`, e.pullID); n != 0 {
				t.Errorf("links = %d, want 0", n)
			}
		})
	}
}
