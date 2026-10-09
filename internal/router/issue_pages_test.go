package router_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type pagesIssue struct {
	number int
	title  string
	id     int64
}

// seedPageIssue adds an issue to the repo, created age seconds ago.
func (e metaEnv) seedPageIssue(t *testing.T, number int, title, state, visibility string, authorID int64, age int) pagesIssue {
	t.Helper()
	var id int64
	err := e.db.QueryRow(
		`INSERT INTO issues (repo_id, number, author_id, title, body, state, visibility, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, '', $5, $6, NOW() - make_interval(secs => $7), NOW() - make_interval(secs => $7))
		 RETURNING id`,
		e.repoID, number, authorID, title, state, visibility, age).Scan(&id)
	if err != nil {
		t.Fatalf("seed issue %d: %v", number, err)
	}
	return pagesIssue{number: number, title: title, id: id}
}

func (e metaEnv) pagesSQL(t *testing.T, query string, args ...any) {
	t.Helper()
	testutil.Exec(t, e.db, query, args...)
}

// pagesExecRepo runs a statement whose only argument is the repository id.
func (e metaEnv) pagesExecRepo(t *testing.T, query string) {
	t.Helper()
	testutil.Exec(t, e.db, query, e.repoID)
}

func (e metaEnv) pagesBody(t *testing.T, suffix, token string) string {
	t.Helper()
	rr := e.do(t, metaReq{method: http.MethodGet, target: fmt.Sprintf("/%s/%s", e.owner.name, e.repoName) + suffix, token: token})
	return rr.Body.String()
}

func (e metaEnv) pagesStatus(t *testing.T, suffix, token string) int {
	t.Helper()
	return e.do(t, metaReq{method: http.MethodGet, target: fmt.Sprintf("/%s/%s", e.owner.name, e.repoName) + suffix, token: token}).Code
}

func TestIssuePages_ListFiltersStateSearchLabelAndMilestone(t *testing.T) {
	e := newMetaEnv(t)
	alpha := e.seedPageIssue(t, 10, "Alpha crash on start", "open", "public", e.owner.id, 300)
	beta := e.seedPageIssue(t, 11, "Beta slow query", "open", "public", e.owner.id, 200)
	gamma := e.seedPageIssue(t, 12, "Gamma finished", "closed", "public", e.owner.id, 100)
	bug := e.seedLabel(t, "bug")
	e.pagesSQL(t, `INSERT INTO issue_labels (issue_id, label_id) VALUES ($1, $2)`, alpha.id, bug)
	var ms int64
	if err := e.db.QueryRow(`INSERT INTO milestones (repo_id, number, title) VALUES ($1, 1, 'v1') RETURNING id`, e.repoID).Scan(&ms); err != nil {
		t.Fatal(err)
	}
	e.pagesSQL(t, `UPDATE issues SET milestone_id = $1 WHERE id = $2`, ms, beta.id)

	has := func(body string, i pagesIssue) bool { return strings.Contains(body, i.title) }

	open := e.pagesBody(t, "/issues", "")
	if !has(open, alpha) || !has(open, beta) || has(open, gamma) {
		t.Error("default list should show open issues only")
	}
	closed := e.pagesBody(t, "/issues?state=closed", "")
	if has(closed, alpha) || !has(closed, gamma) {
		t.Error("state=closed should show closed issues only")
	}
	if body := e.pagesBody(t, "/issues?state=bogus", ""); !has(body, alpha) || has(body, gamma) {
		t.Error("an unknown state falls back to open")
	}
	if body := e.pagesBody(t, "/issues?q=SLOW", ""); has(body, alpha) || !has(body, beta) {
		t.Error("search is a case-insensitive title match")
	}
	if body := e.pagesBody(t, "/issues?label=bug", ""); !has(body, alpha) || has(body, beta) {
		t.Error("label filter should keep only labelled issues")
	}
	if body := e.pagesBody(t, fmt.Sprintf("/issues?milestone=%d", ms), ""); has(body, alpha) || !has(body, beta) {
		t.Error("milestone filter should keep only that milestone's issues")
	}
	if body := e.pagesBody(t, "/issues?milestone=abc", ""); !has(body, alpha) || !has(body, beta) {
		t.Error("a non-numeric milestone filter is ignored")
	}
}

func TestIssuePages_ListSortOrders(t *testing.T) {
	e := newMetaEnv(t)
	e.seedPageIssue(t, 10, "OldestIssue", "open", "public", e.owner.id, 900)
	e.seedPageIssue(t, 11, "MiddleIssue", "open", "public", e.owner.id, 500)
	e.seedPageIssue(t, 12, "NewestIssue", "open", "public", e.owner.id, 100)
	e.pagesExecRepo(t, `UPDATE issues SET updated_at = NOW() + INTERVAL '1 hour' WHERE repo_id = $1 AND number = 10`)

	order := func(sort string) []string {
		body := e.pagesBody(t, "/issues?sort="+sort, "")
		type pos struct {
			name string
			at   int
		}
		var ps []pos
		for _, n := range []string{"OldestIssue", "MiddleIssue", "NewestIssue"} {
			ps = append(ps, pos{n, strings.Index(body, n)})
		}
		for i := range ps {
			for j := i + 1; j < len(ps); j++ {
				if ps[j].at < ps[i].at {
					ps[i], ps[j] = ps[j], ps[i]
				}
			}
		}
		names := make([]string, len(ps))
		for i, p := range ps {
			names[i] = p.name
		}
		return names
	}
	want := map[string]string{
		"":                 "NewestIssue,MiddleIssue,OldestIssue",
		"newest":           "NewestIssue,MiddleIssue,OldestIssue",
		"oldest":           "OldestIssue,MiddleIssue,NewestIssue",
		"recently-updated": "OldestIssue,NewestIssue,MiddleIssue",
	}
	for sort, w := range want {
		if got := strings.Join(order(sort), ","); got != w {
			t.Errorf("sort=%q order = %s, want %s", sort, got, w)
		}
	}
}

func TestIssuePages_PrivateIssuesAreOnlyForAuthorAndWriters(t *testing.T) {
	e := newMetaEnv(t)
	secret := e.seedPageIssue(t, 20, "Embargoed vulnerability", "open", "private", e.outsider.id, 10)
	e.pagesExecRepo(t, `UPDATE issues SET body = 'details of the hole' WHERE repo_id = $1 AND number = 20`)
	detail := fmt.Sprintf("/issues/%d", secret.number)

	for _, c := range []struct {
		name, token string
		visible     bool
	}{
		{"anonymous", "", false},
		{"author", e.outsider.token, true},
		{"owner", e.owner.token, true},
		{"writer", e.writer.token, true},
	} {
		if got := strings.Contains(e.pagesBody(t, "/issues", c.token), secret.title); got != c.visible {
			t.Errorf("%s: listed = %v, want %v", c.name, got, c.visible)
		}
		wantCode := http.StatusNotFound
		if c.visible {
			wantCode = http.StatusOK
		}
		if got := e.pagesStatus(t, detail, c.token); got != wantCode {
			t.Errorf("%s: detail status = %d, want %d", c.name, got, wantCode)
		}
	}
}

func TestIssuePages_DetailRendersBodyCommentsAndLabels(t *testing.T) {
	e := newMetaEnv(t)
	e.pagesSQL(t, `UPDATE issues SET body = $2 WHERE id = $1`, e.issueID, "**bold claim** with `code`")
	label := e.seedLabel(t, "needs-triage")
	e.pagesSQL(t, `INSERT INTO issue_labels (issue_id, label_id) VALUES ($1, $2)`, e.issueID, label)

	rr := e.do(t, metaReq{method: http.MethodGet, target: "/" + e.owner.name + "/" + e.repoName + "/issues/1"})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "<strong>bold claim</strong>")
	bodyHas(t, rr, "hello")
	bodyHas(t, rr, "needs-triage")

	wantStatus(t, e.do(t, metaReq{method: http.MethodGet, target: "/" + e.owner.name + "/" + e.repoName + "/issues/abc"}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: http.MethodGet, target: "/" + e.owner.name + "/" + e.repoName + "/issues/999"}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: http.MethodGet, target: "/" + e.owner.name + "/nope/issues/1"}), http.StatusNotFound)
}

func TestIssuePages_DisabledIssuesAre404(t *testing.T) {
	e := newMetaEnv(t)
	e.pagesExecRepo(t, `UPDATE repositories SET allow_issues = false WHERE id = $1`)
	for _, suffix := range []string{"/issues", "/issues/1", "/issues/new"} {
		if got := e.pagesStatus(t, suffix, e.owner.token); got != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", suffix, got)
		}
	}
	rr := e.do(t, metaReq{method: http.MethodPost, target: "/" + e.owner.name + "/" + e.repoName + "/issues/new", token: e.owner.token, form: url.Values{"title": {"x"}}})
	wantStatus(t, rr, http.StatusNotFound)
	if n := e.count(t, `SELECT COUNT(*) FROM issues WHERE repo_id = $1 AND title = 'x'`, e.repoID); n != 0 {
		t.Errorf("an issue was created while issues are disabled")
	}
}

func TestIssuePages_PrivateRepoPagesAreHiddenFromStrangers(t *testing.T) {
	e := newMetaEnv(t)
	e.pagesExecRepo(t, `UPDATE repositories SET private = true WHERE id = $1`)
	for _, suffix := range []string{"/issues", "/issues/1"} {
		wantStatus(t, e.do(t, metaReq{method: http.MethodGet, target: "/" + e.owner.name + "/" + e.repoName + suffix}), http.StatusNotFound)
		if got := e.pagesStatus(t, suffix, e.outsider.token); got != http.StatusNotFound {
			t.Errorf("outsider GET %s = %d, want 404", suffix, got)
		}
		if got := e.pagesStatus(t, suffix, e.writer.token); got != http.StatusOK {
			t.Errorf("writer GET %s = %d, want 200", suffix, got)
		}
	}
	wantStatus(t, e.do(t, metaReq{method: http.MethodGet, target: "/" + e.owner.name + "/" + e.repoName + "/issues/new", token: e.outsider.token}), http.StatusNotFound)
	rr := e.do(t, metaReq{method: http.MethodPost, target: "/" + e.owner.name + "/" + e.repoName + "/issues/new", token: e.outsider.token, form: url.Values{"title": {"sneaky"}}})
	wantStatus(t, rr, http.StatusNotFound)
}

func TestIssuePages_NewIssueFormAndSubmit(t *testing.T) {
	e := newMetaEnv(t)
	base := "/" + e.owner.name + "/" + e.repoName + "/issues/new"
	submit := func(token string, form url.Values) *httptest.ResponseRecorder {
		return e.do(t, metaReq{method: http.MethodPost, target: base, token: token, form: form})
	}

	toLogin := "/login?next=" + url.QueryEscape(base)
	if loc := e.do(t, metaReq{method: http.MethodGet, target: base}); loc.Code != http.StatusSeeOther || loc.Header().Get("Location") != toLogin {
		t.Errorf("anonymous GET new = %d to %q, want redirect to %q", loc.Code, loc.Header().Get("Location"), toLogin)
	}
	if rr := submit("", url.Values{"title": {"x"}}); rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != toLogin {
		t.Errorf("anonymous submit = %d to %q", rr.Code, rr.Header().Get("Location"))
	}

	wantStatus(t, e.do(t, metaReq{method: http.MethodGet, target: base, token: e.outsider.token}), http.StatusOK)
	wantStatus(t, e.do(t, metaReq{method: http.MethodGet, target: base + "?blank=1&template=nope", token: e.writer.token}), http.StatusOK)

	rr := submit(e.outsider.token, url.Values{"title": {""}, "body": {"kept body"}})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "Title is required")
	bodyHas(t, rr, "kept body")

	rr = submit(e.outsider.token, url.Values{"title": {strings.Repeat("t", 300)}})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "Title is too long")

	rr = submit(e.outsider.token, url.Values{"title": {"Outsider asks"}, "body": {"b"}, "visibility": {"private"}})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "Only collaborators with write access can create private issues")

	rr = submit(e.outsider.token, url.Values{"title": {"Outsider asks"}, "body": {"b"}, "priority": {"P0"}, "labels": {"1"}})
	wantStatus(t, rr, http.StatusSeeOther)
	loc := rr.Header().Get("Location")
	if !strings.HasPrefix(loc, "/"+e.owner.name+"/"+e.repoName+"/issues/") || strings.Contains(loc, "warn") {
		t.Errorf("Location = %q", loc)
	}
	var priority *string
	var vis string
	if err := e.db.QueryRow(`SELECT priority, visibility FROM issues WHERE repo_id = $1 AND title = 'Outsider asks'`, e.repoID).Scan(&priority, &vis); err != nil {
		t.Fatal(err)
	}
	if priority != nil || vis != "public" {
		t.Errorf("priority = %v, visibility = %q; a reader's metadata is ignored", priority, vis)
	}
}

func TestIssuePages_WriterSubmitAppliesMetadata(t *testing.T) {
	e := newMetaEnv(t)
	bug := e.seedLabel(t, "bug")
	var ms int64
	if err := e.db.QueryRow(`INSERT INTO milestones (repo_id, number, title) VALUES ($1, 1, 'v1') RETURNING id`, e.repoID).Scan(&ms); err != nil {
		t.Fatal(err)
	}
	target := "/" + e.owner.name + "/" + e.repoName + "/issues/new"

	rr := e.do(t, metaReq{method: http.MethodPost, target: target, token: e.writer.token, form: url.Values{
		"title":        {"Writer files"},
		"body":         {"body text"},
		"visibility":   {"private"},
		"priority":     {"P2"},
		"labels":       {fmt.Sprint(bug), "junk"},
		"assignees":    {e.owner.name, ""},
		"milestone":    {fmt.Sprint(ms)},
		"linked_pulls": {"1", "x"},
	}})
	wantStatus(t, rr, http.StatusSeeOther)
	if loc := rr.Header().Get("Location"); strings.Contains(loc, "warn=metadata") {
		t.Errorf("Location = %q; every field was valid", loc)
	}
	var id int64
	var priority, vis string
	var milestone *int64
	if err := e.db.QueryRow(`SELECT id, priority, visibility, milestone_id FROM issues WHERE repo_id = $1 AND title = 'Writer files'`, e.repoID).Scan(&id, &priority, &vis, &milestone); err != nil {
		t.Fatal(err)
	}
	if priority != "P2" || vis != "private" || milestone == nil || *milestone != ms {
		t.Errorf("priority=%q visibility=%q milestone=%v", priority, vis, milestone)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM issue_labels WHERE issue_id = $1 AND label_id = $2`, id, bug); n != 1 {
		t.Errorf("label links = %d, want 1", n)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM issue_assignees WHERE issue_id = $1 AND user_id = $2`, id, e.owner.id); n != 1 {
		t.Errorf("assignee links = %d, want 1", n)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM pull_issue_links WHERE issue_id = $1`, id); n != 1 {
		t.Errorf("linked pulls = %d, want 1", n)
	}
}

func TestIssuePages_MetadataFailureStillCreatesTheIssueWithAWarning(t *testing.T) {
	e := newMetaEnv(t)
	rr := e.do(t, metaReq{method: http.MethodPost, target: "/" + e.owner.name + "/" + e.repoName + "/issues/new", token: e.writer.token, form: url.Values{
		"title":     {"Partial"},
		"labels":    {"999999999"},
		"assignees": {"no-such-user"},
		"milestone": {"999999999"},
	}})
	wantStatus(t, rr, http.StatusSeeOther)
	if loc := rr.Header().Get("Location"); !strings.HasSuffix(loc, "?warn=metadata") {
		t.Errorf("Location = %q, want a ?warn=metadata redirect", loc)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM issues WHERE repo_id = $1 AND title = 'Partial'`, e.repoID); n != 1 {
		t.Errorf("issue count = %d, want 1", n)
	}
}
