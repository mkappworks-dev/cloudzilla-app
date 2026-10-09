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

func (e metaEnv) categories(t *testing.T) []int64 {
	t.Helper()
	rows, err := e.db.Query(`SELECT id FROM discussion_categories ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(ids) < 2 {
		t.Fatalf("need two seeded discussion categories, have %d", len(ids))
	}
	return ids
}

func (e metaEnv) startDiscussion(t *testing.T, title string, category int64) int {
	t.Helper()
	rr := e.do(t, metaReq{method: "POST", target: e.path("/discussions"), token: e.writer.token,
		json: fmt.Sprintf(`{"category_id":%d,"title":%q,"body":"body of %s"}`, category, title, title)})
	wantStatus(t, rr, http.StatusCreated)
	var d struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &d); err != nil || d.Number == 0 {
		t.Fatalf("create discussion = %s (%v)", rr.Body.String(), err)
	}
	return d.Number
}

func (e metaEnv) reply(t *testing.T, number int, token, body string) int64 {
	t.Helper()
	rr := e.do(t, metaReq{method: "POST", target: e.path("/discussions/%d/replies", number), token: token, json: fmt.Sprintf(`{"body":%q}`, body)})
	wantStatus(t, rr, http.StatusCreated)
	var r struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &r); err != nil || r.ID == 0 {
		t.Fatalf("create reply = %s (%v)", rr.Body.String(), err)
	}
	return r.ID
}

func (e metaEnv) discussionCol(t *testing.T, number int, col string) string {
	t.Helper()
	var v *string
	if err := e.db.QueryRow(`SELECT `+col+`::text FROM discussions WHERE repo_id = $1 AND number = $2`, e.repoID, number).Scan(&v); err != nil {
		t.Fatalf("discussion %s: %v", col, err)
	}
	if v == nil {
		return ""
	}
	return *v
}

func TestDiscussions_CreateAPI(t *testing.T) {
	e := newGitMetaEnv(t)
	cats := e.categories(t)

	n := e.startDiscussion(t, "first idea", cats[0])
	if n != 1 || e.discussionCol(t, 1, "title") != "first idea" || e.discussionCol(t, 1, "category_id") != fmt.Sprint(cats[0]) {
		t.Fatalf("discussion not stored as #1 in the chosen category")
	}
	if e.startDiscussion(t, "second idea", cats[1]) != 2 {
		t.Error("numbers must be sequential per repo")
	}

	body := func(cat int64, title string) string {
		return fmt.Sprintf(`{"category_id":%d,"title":%q,"body":"b"}`, cat, title)
	}
	cases := []struct {
		name string
		req  metaReq
		want int
	}{
		{"anonymous", metaReq{method: "POST", target: e.path("/discussions"), json: body(cats[0], "x")}, http.StatusUnauthorized},
		{"outsider", metaReq{method: "POST", target: e.path("/discussions"), token: e.outsider.token, json: body(cats[0], "x")}, http.StatusForbidden},
		{"unknown repo", metaReq{method: "POST", target: "/api/repos/" + e.owner.name + "/nope/discussions", token: e.owner.token, json: body(cats[0], "x")}, http.StatusNotFound},
		{"bad json", metaReq{method: "POST", target: e.path("/discussions"), token: e.owner.token, json: `{`}, http.StatusBadRequest},
		{"no title", metaReq{method: "POST", target: e.path("/discussions"), token: e.owner.token, json: body(cats[0], "")}, http.StatusBadRequest},
		{"unknown category", metaReq{method: "POST", target: e.path("/discussions"), token: e.owner.token, json: body(999999999, "x")}, http.StatusBadRequest},
		{"title too long", metaReq{method: "POST", target: e.path("/discussions"), token: e.owner.token, json: body(cats[0], strings.Repeat("t", 1000))}, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantStatus(t, e.do(t, c.req), c.want) })
	}
	if got := e.count(t, `SELECT COUNT(*) FROM discussions WHERE repo_id = $1`, e.repoID); got != 2 {
		t.Errorf("discussions = %d, want 2", got)
	}

	testutil.Exec(t, e.db, `UPDATE repositories SET allow_discussions = FALSE WHERE id = $1`, e.repoID)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/discussions"), token: e.owner.token, json: body(cats[0], "x")}), http.StatusNotFound)
}

func TestDiscussions_Replies(t *testing.T) {
	e := newGitMetaEnv(t)
	cats := e.categories(t)
	n := e.startDiscussion(t, "talk", cats[0])
	target := e.path("/discussions/%d/replies", n)
	count := func() int {
		return e.count(t, `SELECT COUNT(*) FROM discussion_replies r JOIN discussions d ON d.id = r.discussion_id WHERE d.repo_id = $1`, e.repoID)
	}

	first := e.reply(t, n, e.outsider.token, "a reply from a reader")
	rr := e.do(t, metaReq{method: "POST", target: target, token: e.writer.token, htmx: true, form: url.Values{"body": {"**htmx** reply"}, "parent_id": {fmt.Sprint(first)}}})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "<strong>htmx</strong>")
	if got := e.count(t, `SELECT COUNT(*) FROM discussion_replies WHERE parent_id = $1`, first); got != 1 {
		t.Errorf("threaded replies = %d, want 1", got)
	}
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.writer.token, htmx: true, form: url.Values{"body": {"ignored parent"}, "parent_id": {"abc"}}}), http.StatusOK)
	if count() != 3 {
		t.Fatalf("replies = %d, want 3", count())
	}

	cases := []struct {
		name string
		req  metaReq
		want int
	}{
		{"anonymous", metaReq{method: "POST", target: target, json: `{"body":"x"}`}, http.StatusUnauthorized},
		{"bad number", metaReq{method: "POST", target: e.path("/discussions/x/replies"), token: e.owner.token, json: `{"body":"x"}`}, http.StatusBadRequest},
		{"missing discussion", metaReq{method: "POST", target: e.path("/discussions/99/replies"), token: e.owner.token, json: `{"body":"x"}`}, http.StatusNotFound},
		{"bad json", metaReq{method: "POST", target: target, token: e.owner.token, json: `{`}, http.StatusBadRequest},
		{"empty body", metaReq{method: "POST", target: target, token: e.owner.token, json: `{"body":""}`}, http.StatusBadRequest},
		{"empty form body", metaReq{method: "POST", target: target, token: e.owner.token, htmx: true, form: url.Values{"body": {""}}}, http.StatusBadRequest},
		{"unknown repo", metaReq{method: "POST", target: "/api/repos/" + e.owner.name + "/nope/discussions/1/replies", token: e.owner.token, json: `{"body":"x"}`}, http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantStatus(t, e.do(t, c.req), c.want) })
	}
	if count() != 3 {
		t.Errorf("refused requests added replies: %d", count())
	}

	e.makePrivate(t)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.outsider.token, json: `{"body":"x"}`}), http.StatusNotFound)
}

func TestDiscussions_RepliesOnLocked(t *testing.T) {
	e := newGitMetaEnv(t)
	n := e.startDiscussion(t, "locked talk", e.categories(t)[0])
	testutil.Exec(t, e.db, `UPDATE discussions SET is_locked = TRUE WHERE repo_id = $1 AND number = $2`, e.repoID, n)
	target := e.path("/discussions/%d/replies", n)
	count := func() int {
		return e.count(t, `SELECT COUNT(*) FROM discussion_replies r JOIN discussions d ON d.id = r.discussion_id WHERE d.repo_id = $1`, e.repoID)
	}

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.outsider.token, json: `{"body":"x"}`}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.writer.token, json: `{"body":"x"}`}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.outsider.token, htmx: true, form: url.Values{"body": {"x"}}}), http.StatusForbidden)
	if count() != 0 {
		t.Fatalf("replies were added to a locked discussion: %d", count())
	}
	e.reply(t, n, e.owner.token, "maintainers may still reply")
}

func TestDiscussions_DeleteReply(t *testing.T) {
	e := newGitMetaEnv(t)
	cats := e.categories(t)
	n1, n2 := e.startDiscussion(t, "one", cats[0]), e.startDiscussion(t, "two", cats[0])
	r1 := e.reply(t, n1, e.outsider.token, "to delete")
	r2 := e.reply(t, n2, e.outsider.token, "on the other discussion")
	exists := func(id int64) bool {
		return e.count(t, `SELECT COUNT(*) FROM discussion_replies WHERE id = $1`, id) == 1
	}

	cases := []struct {
		name string
		req  metaReq
		want int
	}{
		{"anonymous", metaReq{method: "DELETE", target: e.path("/discussions/%d/replies/%d", n1, r1)}, http.StatusUnauthorized},
		{"outsider, even for their own reply", metaReq{method: "DELETE", target: e.path("/discussions/%d/replies/%d", n1, r1), token: e.outsider.token}, http.StatusForbidden},
		{"bad number", metaReq{method: "DELETE", target: e.path("/discussions/x/replies/%d", r1), token: e.owner.token}, http.StatusBadRequest},
		{"bad reply id", metaReq{method: "DELETE", target: e.path("/discussions/%d/replies/x", n1), token: e.owner.token}, http.StatusBadRequest},
		{"missing discussion", metaReq{method: "DELETE", target: e.path("/discussions/99/replies/%d", r1), token: e.owner.token}, http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantStatus(t, e.do(t, c.req), c.want) })
	}
	if !exists(r1) {
		t.Fatal("refusals deleted the reply")
	}

	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/discussions/%d/replies/%d", n1, r2), token: e.owner.token}), http.StatusNoContent)
	if !exists(r2) {
		t.Error("a reply of another discussion was deleted through this discussion's URL")
	}
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/discussions/%d/replies/%d", n1, r1), token: e.writer.token}), http.StatusNoContent)
	if exists(r1) {
		t.Error("reply not deleted")
	}
}

func TestDiscussions_Patch(t *testing.T) {
	e := newGitMetaEnv(t)
	cats := e.categories(t)
	n := e.startDiscussion(t, "original title", cats[0])
	other := e.startDiscussion(t, "other", cats[0])
	reply := e.reply(t, n, e.outsider.token, "the answer")
	otherReply := e.reply(t, other, e.outsider.token, "elsewhere")
	target := e.path("/discussions/%d", n)

	bad := []struct {
		name string
		req  metaReq
		want int
	}{
		{"anonymous", metaReq{method: "PATCH", target: target, json: `{"locked":true}`}, http.StatusUnauthorized},
		{"outsider", metaReq{method: "PATCH", target: target, token: e.outsider.token, json: `{"locked":true}`}, http.StatusForbidden},
		{"bad number", metaReq{method: "PATCH", target: e.path("/discussions/x"), token: e.owner.token, json: `{"locked":true}`}, http.StatusBadRequest},
		{"missing discussion", metaReq{method: "PATCH", target: e.path("/discussions/99"), token: e.owner.token, json: `{"locked":true}`}, http.StatusNotFound},
		{"bad json", metaReq{method: "PATCH", target: target, token: e.owner.token, json: `{`}, http.StatusBadRequest},
		{"blank title", metaReq{method: "PATCH", target: target, token: e.owner.token, json: `{"title":"   "}`}, http.StatusBadRequest},
		{"title too long", metaReq{method: "PATCH", target: target, token: e.owner.token, json: fmt.Sprintf(`{"title":%q}`, strings.Repeat("t", 1000))}, http.StatusBadRequest},
		{"unknown category", metaReq{method: "PATCH", target: target, token: e.owner.token, json: `{"category_id":999999999}`}, http.StatusUnprocessableEntity},
		{"answer is not a number", metaReq{method: "PATCH", target: target, token: e.owner.token, json: `{"answer_id":"x"}`}, http.StatusBadRequest},
		{"answer from another discussion", metaReq{method: "PATCH", target: target, token: e.owner.token, json: fmt.Sprintf(`{"answer_id":%d}`, otherReply)}, http.StatusBadRequest},
		{"form with bad category", metaReq{method: "PATCH", target: target, token: e.owner.token, htmx: true, form: url.Values{"category_id": {"x"}}}, http.StatusBadRequest},
		{"form with blank title", metaReq{method: "PATCH", target: target, token: e.owner.token, htmx: true, form: url.Values{"title": {" "}}}, http.StatusBadRequest},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) { wantStatus(t, e.do(t, c.req), c.want) })
	}
	if e.discussionCol(t, n, "title") != "original title" || e.discussionCol(t, n, "is_locked") != "false" || e.discussionCol(t, n, "is_answered") != "false" {
		t.Fatal("refusals changed the discussion")
	}

	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.writer.token, json: fmt.Sprintf(`{"answer_id":%d}`, reply)}), http.StatusNoContent)
	if e.discussionCol(t, n, "is_answered") != "true" || e.discussionCol(t, n, "answer_id") != fmt.Sprint(reply) {
		t.Error("answer not marked")
	}
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, json: `{"answer_id":null}`}), http.StatusNoContent)
	if e.discussionCol(t, n, "is_answered") != "false" || e.discussionCol(t, n, "answer_id") != "" {
		t.Error("answer not cleared")
	}

	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, json: `{"locked":true}`}), http.StatusNoContent)
	if e.discussionCol(t, n, "is_locked") != "true" {
		t.Error("discussion not locked")
	}
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, json: fmt.Sprintf(`{"title":"  new title ","body":"new body","category_id":%d,"locked":false}`, cats[1])}), http.StatusNoContent)
	if e.discussionCol(t, n, "title") != "new title" || e.discussionCol(t, n, "body") != "new body" ||
		e.discussionCol(t, n, "category_id") != fmt.Sprint(cats[1]) || e.discussionCol(t, n, "is_locked") != "false" {
		t.Error("content edit not stored")
	}

	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, htmx: true,
		form: url.Values{"answer_id": {fmt.Sprint(reply)}, "locked": {"true"}, "title": {"form title"}, "body": {"form body"}, "category_id": {fmt.Sprint(cats[0])}}}), http.StatusNoContent)
	if e.discussionCol(t, n, "title") != "form title" || e.discussionCol(t, n, "is_locked") != "true" || e.discussionCol(t, n, "is_answered") != "true" {
		t.Error("form edit not stored")
	}
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, htmx: true, form: url.Values{"answer_id": {""}}}), http.StatusNoContent)
	if e.discussionCol(t, n, "is_answered") != "false" {
		t.Error("an empty answer_id form value must clear the answer")
	}
}

func TestDiscussions_ListPage(t *testing.T) {
	e := newGitMetaEnv(t)
	cats := e.categories(t)
	e.startDiscussion(t, "an open topic", cats[0])
	answered := e.startDiscussion(t, "an answered topic", cats[1])
	closed := e.startDiscussion(t, "a closed topic", cats[0])
	reply := e.reply(t, answered, e.outsider.token, "solved it")
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/discussions/%d", answered), token: e.owner.token, json: fmt.Sprintf(`{"answer_id":%d}`, reply)}), http.StatusNoContent)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/discussions/%d", closed), token: e.owner.token, json: `{"locked":true}`}), http.StatusNoContent)

	list := func(query, token string) string {
		rr := e.page(t, e.pagePath("/discussions")+query, token, false)
		wantStatus(t, rr, http.StatusOK)
		return rr.Body.String()
	}
	if body := list("", ""); !strings.Contains(body, "an open topic") || strings.Contains(body, "an answered topic") || strings.Contains(body, "a closed topic") {
		t.Error("the default tab must list only open discussions")
	}
	if body := list("?state=answered", e.owner.token); !strings.Contains(body, "an answered topic") || strings.Contains(body, "an open topic") {
		t.Error("answered tab lists the wrong discussions")
	}
	if body := list("?state=closed", ""); !strings.Contains(body, "a closed topic") || strings.Contains(body, "an open topic") {
		t.Error("closed tab lists the wrong discussions")
	}
	if body := list(fmt.Sprintf("?category=%d", cats[1]), ""); strings.Contains(body, "an open topic") {
		t.Error("category filter still lists discussions of other categories")
	}
	list("?state=bogus&category=abc", "")
	list(fmt.Sprintf("?category=%d&state=answered", cats[1]), "")

	wantStatus(t, e.page(t, "/"+e.owner.name+"/nope/discussions", "", false), http.StatusNotFound)
	testutil.Exec(t, e.db, `UPDATE repositories SET allow_discussions = FALSE WHERE id = $1`, e.repoID)
	wantStatus(t, e.page(t, e.pagePath("/discussions"), "", false), http.StatusNotFound)
	testutil.Exec(t, e.db, `UPDATE repositories SET allow_discussions = TRUE, private = TRUE WHERE id = $1`, e.repoID)
	wantStatus(t, e.page(t, e.pagePath("/discussions"), e.outsider.token, false), http.StatusNotFound)
}

func TestDiscussions_DetailPage(t *testing.T) {
	e := newGitMetaEnv(t)
	cats := e.categories(t)
	n := e.startDiscussion(t, "detail topic", cats[0])
	e.reply(t, n, e.outsider.token, "first *reply*")
	e.reply(t, n, e.owner.token, "second reply")

	rr := e.page(t, e.pagePath("/discussions/%d", n), "", false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "detail topic")
	bodyHas(t, rr, "body of detail topic")
	bodyHas(t, rr, "<em>reply</em>")
	bodyHas(t, rr, "second reply")
	wantStatus(t, e.page(t, e.pagePath("/discussions/%d", n), e.writer.token, false), http.StatusOK)
	wantStatus(t, e.page(t, e.pagePath("/discussions/x"), "", false), http.StatusBadRequest)
	wantStatus(t, e.page(t, e.pagePath("/discussions/99"), "", false), http.StatusNotFound)
	wantStatus(t, e.page(t, "/"+e.owner.name+"/nope/discussions/1", "", false), http.StatusNotFound)

	e.makePrivate(t)
	wantStatus(t, e.page(t, e.pagePath("/discussions/%d", n), e.outsider.token, false), http.StatusNotFound)
	wantStatus(t, e.page(t, e.pagePath("/discussions/%d", n), "", false), http.StatusNotFound)
	testutil.Exec(t, e.db, `UPDATE repositories SET allow_discussions = FALSE WHERE id = $1`, e.repoID)
	wantStatus(t, e.page(t, e.pagePath("/discussions/%d", n), e.owner.token, false), http.StatusNotFound)
}

func TestDiscussions_NewPageAndSubmit(t *testing.T) {
	e := newGitMetaEnv(t)
	cats := e.categories(t)
	newPage := e.pagePath("/discussions/new")

	rr := e.page(t, newPage, "", false)
	if rr.Code != http.StatusSeeOther && rr.Code != http.StatusUnauthorized {
		t.Errorf("anonymous new page = %d", rr.Code)
	}
	wantStatus(t, e.page(t, newPage, e.outsider.token, false), http.StatusForbidden)
	wantStatus(t, e.page(t, "/"+e.owner.name+"/nope/discussions/new", e.owner.token, false), http.StatusNotFound)
	wantStatus(t, e.page(t, newPage, e.writer.token, false), http.StatusOK)
	wantStatus(t, e.page(t, newPage+fmt.Sprintf("?category=%d", cats[1]), e.writer.token, false), http.StatusOK)
	wantStatus(t, e.page(t, newPage+"?category=garbage", e.writer.token, false), http.StatusOK)

	submit := func(token string, form url.Values) *httpResult {
		rr := e.do(t, metaReq{method: "POST", target: newPage, token: token, form: form})
		return &httpResult{rr.Code, rr.Header().Get("Location"), rr.Body.String()}
	}
	cat := fmt.Sprint(cats[0])
	if r := submit("", url.Values{"title": {"x"}, "category_id": {cat}}); r.code != http.StatusSeeOther && r.code != http.StatusUnauthorized {
		t.Errorf("anonymous submit = %+v", r)
	}
	if r := submit(e.outsider.token, url.Values{"title": {"x"}, "category_id": {cat}}); r.code != http.StatusForbidden {
		t.Errorf("outsider submit = %d", r.code)
	}
	if r := submit(e.owner.token, url.Values{"title": {""}, "category_id": {cat}}); r.code != http.StatusOK || !strings.Contains(r.body, "Title is required") {
		t.Errorf("blank title = %d %.200s", r.code, r.body)
	}
	if r := submit(e.owner.token, url.Values{"title": {"t"}}); r.code != http.StatusOK || !strings.Contains(r.body, "Pick a category") {
		t.Errorf("missing category = %d %.200s", r.code, r.body)
	}
	if r := submit(e.owner.token, url.Values{"title": {"t"}, "category_id": {"999999999"}}); r.code != http.StatusOK || !strings.Contains(r.body, "Pick a category") {
		t.Errorf("unknown category = %d %.200s", r.code, r.body)
	}
	if r := submit(e.owner.token, url.Values{"title": {"t"}, "category_id": {"abc"}}); r.code != http.StatusBadRequest {
		t.Errorf("non-numeric category = %d", r.code)
	}
	if r := submit(e.owner.token, url.Values{"title": {strings.Repeat("t", 1000)}, "category_id": {cat}}); !strings.Contains(r.body, "too long") {
		t.Errorf("long title = %d %.200s", r.code, r.body)
	}
	if got := e.count(t, `SELECT COUNT(*) FROM discussions WHERE repo_id = $1`, e.repoID); got != 0 {
		t.Fatalf("invalid submissions created %d discussions", got)
	}

	r := submit(e.writer.token, url.Values{"title": {"Real topic"}, "body": {"details"}, "category_id": {cat}})
	if r.code != http.StatusSeeOther || r.location != "/"+e.owner.name+"/"+e.repoName+"/discussions/1" {
		t.Fatalf("submit = %+v", r)
	}
	if e.discussionCol(t, 1, "title") != "Real topic" {
		t.Error("discussion not stored")
	}

	testutil.Exec(t, e.db, `UPDATE repositories SET allow_discussions = FALSE WHERE id = $1`, e.repoID)
	wantStatus(t, e.page(t, newPage, e.owner.token, false), http.StatusNotFound)
	if r := submit(e.owner.token, url.Values{"title": {"x"}, "category_id": {cat}}); r.code != http.StatusNotFound {
		t.Errorf("submit with discussions disabled = %d, want 404", r.code)
	}
}
