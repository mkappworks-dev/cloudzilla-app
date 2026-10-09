package router_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func (e metaEnv) seedIssueComment(t *testing.T, issueID, authorID int64, body string) int64 {
	t.Helper()
	var id int64
	err := e.db.QueryRow(`INSERT INTO comments (repo_id, issue_id, author_id, body) VALUES ($1, $2, $3, $4) RETURNING id`, e.repoID, issueID, authorID, body).Scan(&id)
	if err != nil {
		t.Fatalf("seed issue comment: %v", err)
	}
	return id
}

func (e metaEnv) seedPullComment(t *testing.T, pullID, authorID int64, body string) int64 {
	t.Helper()
	var id int64
	err := e.db.QueryRow(`INSERT INTO comments (repo_id, pull_id, author_id, body) VALUES ($1, $2, $3, $4) RETURNING id`, e.repoID, pullID, authorID, body).Scan(&id)
	if err != nil {
		t.Fatalf("seed pull comment: %v", err)
	}
	return id
}

func (e metaEnv) commentBody(t *testing.T, id int64) string {
	t.Helper()
	var b string
	if err := e.db.QueryRow(`SELECT body FROM comments WHERE id = $1`, id).Scan(&b); err != nil {
		return ""
	}
	return b
}

func (e metaEnv) commentExists(t *testing.T, id int64) bool {
	t.Helper()
	return e.count(t, `SELECT COUNT(*) FROM comments WHERE id = $1`, id) == 1
}

func TestComments_ListAndFragment(t *testing.T) {
	e := newGitMetaEnv(t)
	issueID, n := e.seedIssue(t, "talk", "open")
	e.seedIssueComment(t, issueID, e.owner.id, "first *comment*")
	e.seedIssueComment(t, issueID, e.writer.id, "second comment")

	rr := e.do(t, metaReq{method: "GET", target: e.path("/issues/%d/comments", n)})
	wantStatus(t, rr, http.StatusOK)
	var list []struct{ Body string }
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil || len(list) != 2 {
		t.Fatalf("comments = %s (%v)", rr.Body.String(), err)
	}

	_, empty := e.seedIssue(t, "quiet", "open")
	rr = e.do(t, metaReq{method: "GET", target: e.path("/issues/%d/comments", empty)})
	wantStatus(t, rr, http.StatusOK)

	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/issues/x/comments")}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/issues/99/comments")}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: "/api/repos/" + e.owner.name + "/nope/issues/1/comments"}), http.StatusNotFound)

	frag := "/fragments/" + e.owner.name + "/" + e.repoName + "/issues/"
	rr = e.do(t, metaReq{method: "GET", target: frag + "1/comments"})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "<em>comment</em>")
	bodyHas(t, rr, "second comment")
	wantStatus(t, e.do(t, metaReq{method: "GET", target: frag + "2/comments"}), http.StatusOK)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: frag + "x/comments"}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: frag + "99/comments"}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: "/fragments/" + e.owner.name + "/nope/issues/1/comments"}), http.StatusNotFound)

	privID, privN := e.seedIssue(t, "private talk", "open")
	testutil.Exec(t, e.db, `UPDATE issues SET visibility = 'private' WHERE id = $1`, privID)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/issues/%d/comments", privN), token: e.outsider.token}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: frag + "3/comments", token: e.outsider.token}), http.StatusNotFound)

	e.makePrivate(t)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/issues/%d/comments", n), token: e.outsider.token}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: frag + "1/comments", token: e.outsider.token}), http.StatusNotFound)
}

func TestComments_CreateOnIssue(t *testing.T) {
	e := newGitMetaEnv(t)
	issueID, n := e.seedIssue(t, "talk", "open")
	target := e.path("/issues/%d/comments", n)
	count := func() int { return e.count(t, `SELECT COUNT(*) FROM comments WHERE issue_id = $1`, issueID) }

	rr := e.do(t, metaReq{method: "POST", target: target, token: e.outsider.token, json: `{"body":"hello there"}`})
	wantStatus(t, rr, http.StatusCreated)
	var got struct {
		ID       int64 `json:"id"`
		AuthorID int64 `json:"author_id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil || got.AuthorID != e.outsider.id {
		t.Fatalf("created = %s (%v)", rr.Body.String(), err)
	}
	if e.commentBody(t, got.ID) != "hello there" {
		t.Error("comment body not stored")
	}

	rr = e.do(t, metaReq{method: "POST", target: target, token: e.writer.token, htmx: true, form: url.Values{"body": {"**strong** reply"}}})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "<strong>strong</strong>")
	if count() != 2 {
		t.Errorf("comments = %d, want 2", count())
	}

	cases := []struct {
		name string
		req  metaReq
		want int
	}{
		{"anonymous", metaReq{method: "POST", target: target, json: `{"body":"x"}`}, http.StatusUnauthorized},
		{"bad number", metaReq{method: "POST", target: e.path("/issues/x/comments"), token: e.owner.token, json: `{"body":"x"}`}, http.StatusBadRequest},
		{"bad json", metaReq{method: "POST", target: target, token: e.owner.token, json: `{`}, http.StatusBadRequest},
		{"blank body", metaReq{method: "POST", target: target, token: e.owner.token, json: `{"body":"  \n "}`}, http.StatusBadRequest},
		{"blank form body", metaReq{method: "POST", target: target, token: e.owner.token, htmx: true, form: url.Values{"body": {""}}}, http.StatusBadRequest},
		{"missing issue", metaReq{method: "POST", target: e.path("/issues/99/comments"), token: e.owner.token, json: `{"body":"x"}`}, http.StatusNotFound},
		{"unknown repo", metaReq{method: "POST", target: "/api/repos/" + e.owner.name + "/nope/issues/1/comments", token: e.owner.token, json: `{"body":"x"}`}, http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantStatus(t, e.do(t, c.req), c.want) })
	}
	if count() != 2 {
		t.Errorf("refused requests added comments: %d", count())
	}
}

func TestComments_LockedIssueOnlyManagersComment(t *testing.T) {
	e := newGitMetaEnv(t)
	issueID, n := e.seedIssue(t, "locked", "open")
	testutil.Exec(t, e.db, `UPDATE issues SET is_locked = TRUE WHERE id = $1`, issueID)
	target := e.path("/issues/%d/comments", n)

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.outsider.token, json: `{"body":"x"}`}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.writer.token, json: `{"body":"x"}`}), http.StatusForbidden)
	if n := e.count(t, `SELECT COUNT(*) FROM comments WHERE issue_id = $1`, issueID); n != 0 {
		t.Fatalf("locked issue got %d comments from non-managers", n)
	}
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, json: `{"body":"maintainer note"}`}), http.StatusCreated)
}

func TestComments_CreateOnPull(t *testing.T) {
	e := newGitMetaEnv(t)
	pullID, n := e.seedPull(t, "change", "open")
	target := e.path("/pulls/%d/comments", n)
	count := func() int { return e.count(t, `SELECT COUNT(*) FROM comments WHERE pull_id = $1`, pullID) }

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.outsider.token, json: `{"body":"lgtm"}`}), http.StatusCreated)
	rr := e.do(t, metaReq{method: "POST", target: target, token: e.owner.token, htmx: true, form: url.Values{"body": {"thanks"}}})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "thanks")
	if !strings.Contains(rr.Header().Get("HX-Trigger"), "Comment added") {
		t.Errorf("HX-Trigger = %q", rr.Header().Get("HX-Trigger"))
	}
	if count() != 2 {
		t.Fatalf("comments = %d, want 2", count())
	}

	cases := []struct {
		name string
		req  metaReq
		want int
	}{
		{"anonymous", metaReq{method: "POST", target: target, json: `{"body":"x"}`}, http.StatusUnauthorized},
		{"bad number", metaReq{method: "POST", target: e.path("/pulls/x/comments"), token: e.owner.token, json: `{"body":"x"}`}, http.StatusBadRequest},
		{"bad json", metaReq{method: "POST", target: target, token: e.owner.token, json: `{`}, http.StatusBadRequest},
		{"blank", metaReq{method: "POST", target: target, token: e.owner.token, json: `{"body":" "}`}, http.StatusBadRequest},
		{"missing pull", metaReq{method: "POST", target: e.path("/pulls/99/comments"), token: e.owner.token, json: `{"body":"x"}`}, http.StatusNotFound},
		{"unknown repo", metaReq{method: "POST", target: "/api/repos/" + e.owner.name + "/nope/pulls/1/comments", token: e.owner.token, json: `{"body":"x"}`}, http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantStatus(t, e.do(t, c.req), c.want) })
	}
	if count() != 2 {
		t.Errorf("refused requests added comments: %d", count())
	}
}

func TestComments_Update(t *testing.T) {
	e := newGitMetaEnv(t)
	issueID, n := e.seedIssue(t, "talk", "open")
	_, otherN := e.seedIssue(t, "other", "open")
	pullID, pullN := e.seedPull(t, "change", "open")
	mine := e.seedIssueComment(t, issueID, e.writer.id, "original")
	pullMine := e.seedPullComment(t, pullID, e.writer.id, "pull original")
	target := e.path("/issues/%d/comments/%d", n, mine)

	cases := []struct {
		name string
		req  metaReq
		want int
	}{
		{"anonymous", metaReq{method: "PATCH", target: target, json: `{"body":"x"}`}, http.StatusUnauthorized},
		{"not the author, even an owner", metaReq{method: "PATCH", target: target, token: e.owner.token, json: `{"body":"x"}`}, http.StatusForbidden},
		{"outsider", metaReq{method: "PATCH", target: target, token: e.outsider.token, json: `{"body":"x"}`}, http.StatusForbidden},
		{"bad id", metaReq{method: "PATCH", target: e.path("/issues/%d/comments/x", n), token: e.writer.token, json: `{"body":"x"}`}, http.StatusBadRequest},
		{"bad json", metaReq{method: "PATCH", target: target, token: e.writer.token, json: `{`}, http.StatusBadRequest},
		{"blank body", metaReq{method: "PATCH", target: target, token: e.writer.token, json: `{"body":""}`}, http.StatusBadRequest},
		{"unknown comment", metaReq{method: "PATCH", target: e.path("/issues/%d/comments/999999999", n), token: e.writer.token, json: `{"body":"x"}`}, http.StatusNotFound},
		{"comment of another issue", metaReq{method: "PATCH", target: e.path("/issues/%d/comments/%d", otherN, mine), token: e.writer.token, json: `{"body":"x"}`}, http.StatusNotFound},
		{"issue comment through the pull route", metaReq{method: "PATCH", target: e.path("/pulls/%d/comments/%d", pullN, mine), token: e.writer.token, json: `{"body":"x"}`}, http.StatusNotFound},
		{"unknown repo", metaReq{method: "PATCH", target: "/api/repos/" + e.owner.name + "/nope/issues/1/comments/1", token: e.writer.token, json: `{"body":"x"}`}, http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantStatus(t, e.do(t, c.req), c.want) })
	}
	if e.commentBody(t, mine) != "original" || e.commentBody(t, pullMine) != "pull original" {
		t.Fatal("refusals edited a comment")
	}

	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.writer.token, json: `{"body":"edited"}`}), http.StatusOK)
	if e.commentBody(t, mine) != "edited" {
		t.Error("comment not edited")
	}
	rr := e.do(t, metaReq{method: "PATCH", target: target, token: e.writer.token, htmx: true, form: url.Values{"body": {"_htmx_ edit"}}})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "<em>htmx</em>")
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/pulls/%d/comments/%d", pullN, pullMine), token: e.writer.token, json: `{"body":"pull edited"}`}), http.StatusOK)
	if e.commentBody(t, pullMine) != "pull edited" {
		t.Error("pull comment not edited")
	}
}

func TestComments_Delete(t *testing.T) {
	e := newGitMetaEnv(t)
	issueID, n := e.seedIssue(t, "talk", "open")
	_, otherN := e.seedIssue(t, "other", "open")
	pullID, pullN := e.seedPull(t, "change", "open")
	byOutsider := e.seedIssueComment(t, issueID, e.outsider.id, "from outsider")
	byOwner := e.seedIssueComment(t, issueID, e.owner.id, "from owner")
	pullComment := e.seedPullComment(t, pullID, e.outsider.id, "on pull")

	cases := []struct {
		name string
		req  metaReq
		want int
	}{
		{"anonymous", metaReq{method: "DELETE", target: e.path("/issues/%d/comments/%d", n, byOwner)}, http.StatusUnauthorized},
		{"outsider deleting another's", metaReq{method: "DELETE", target: e.path("/issues/%d/comments/%d", n, byOwner), token: e.outsider.token}, http.StatusForbidden},
		{"bad id", metaReq{method: "DELETE", target: e.path("/issues/%d/comments/x", n), token: e.owner.token}, http.StatusBadRequest},
		{"unknown comment", metaReq{method: "DELETE", target: e.path("/issues/%d/comments/999999999", n), token: e.owner.token}, http.StatusNotFound},
		{"wrong issue", metaReq{method: "DELETE", target: e.path("/issues/%d/comments/%d", otherN, byOwner), token: e.owner.token}, http.StatusNotFound},
		{"issue comment via pull route", metaReq{method: "DELETE", target: e.path("/pulls/%d/comments/%d", pullN, byOwner), token: e.owner.token}, http.StatusNotFound},
		{"unknown repo", metaReq{method: "DELETE", target: "/api/repos/" + e.owner.name + "/nope/issues/1/comments/1", token: e.owner.token}, http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantStatus(t, e.do(t, c.req), c.want) })
	}
	if !e.commentExists(t, byOwner) || !e.commentExists(t, byOutsider) || !e.commentExists(t, pullComment) {
		t.Fatal("refusals deleted a comment")
	}

	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/issues/%d/comments/%d", n, byOutsider), token: e.outsider.token}), http.StatusNoContent)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/issues/%d/comments/%d", n, byOwner), token: e.writer.token}), http.StatusNoContent)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/pulls/%d/comments/%d", pullN, pullComment), token: e.owner.token}), http.StatusNoContent)
	if e.commentExists(t, byOwner) || e.commentExists(t, byOutsider) || e.commentExists(t, pullComment) {
		t.Error("comments were not deleted")
	}
}

func TestComments_DeleteInOtherRepoLeavesItAlone(t *testing.T) {
	e := newGitMetaEnv(t)
	other := newGitMetaEnv(t)
	oIssue, oN := other.seedIssue(t, "theirs", "open")
	foreign := other.seedIssueComment(t, oIssue, other.owner.id, "theirs")
	_, n := e.seedIssue(t, "mine", "open")

	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/issues/%d/comments/%d", n, foreign), token: e.owner.token}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/issues/%d/comments/%d", oN, foreign), token: e.owner.token, json: `{"body":"hijack"}`}), http.StatusNotFound)
	if !other.commentExists(t, foreign) || other.commentBody(t, foreign) != "theirs" {
		t.Error("a comment of another repo was changed through this repo's URL")
	}
}
