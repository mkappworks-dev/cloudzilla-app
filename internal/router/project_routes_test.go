package router_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func (e metaEnv) createProject(t *testing.T, name string) int64 {
	t.Helper()
	rr := e.do(t, metaReq{method: "POST", target: e.path("/projects"), token: e.writer.token, json: `{"name":"` + name + `","description":"about ` + name + `"}`})
	wantStatus(t, rr, http.StatusCreated)
	var p struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &p); err != nil || p.ID == 0 {
		t.Fatalf("create project = %s (%v)", rr.Body.String(), err)
	}
	return p.ID
}

func (e metaEnv) createColumn(t *testing.T, project int64, name string) int64 {
	t.Helper()
	rr := e.do(t, metaReq{method: "POST", target: e.path("/projects/%d/columns", project), token: e.writer.token, json: `{"name":"` + name + `"}`})
	wantStatus(t, rr, http.StatusCreated)
	var c struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &c); err != nil || c.ID == 0 {
		t.Fatalf("create column = %s (%v)", rr.Body.String(), err)
	}
	return c.ID
}

func (e metaEnv) createNote(t *testing.T, project, column int64, note string) int64 {
	t.Helper()
	rr := e.do(t, metaReq{method: "POST", target: e.path("/projects/%d/cards", project), token: e.writer.token,
		json: fmt.Sprintf(`{"column_id":%d,"title":%q}`, column, note)})
	wantStatus(t, rr, http.StatusCreated)
	var c struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &c); err != nil || c.ID == 0 {
		t.Fatalf("create card = %s (%v)", rr.Body.String(), err)
	}
	return c.ID
}

func TestProjects_Create(t *testing.T) {
	e := newGitMetaEnv(t)
	id := e.createProject(t, "Roadmap")
	if n := e.count(t, `SELECT COUNT(*) FROM projects WHERE id = $1 AND repo_id = $2 AND name = 'Roadmap' AND description = 'about Roadmap'`, id, e.repoID); n != 1 {
		t.Fatal("project not stored")
	}

	cases := []struct {
		name string
		req  metaReq
		want int
	}{
		{"anonymous", metaReq{method: "POST", target: e.path("/projects"), json: `{"name":"x"}`}, http.StatusUnauthorized},
		{"outsider", metaReq{method: "POST", target: e.path("/projects"), token: e.outsider.token, json: `{"name":"x"}`}, http.StatusForbidden},
		{"bad json", metaReq{method: "POST", target: e.path("/projects"), token: e.owner.token, json: `{`}, http.StatusBadRequest},
		{"no name", metaReq{method: "POST", target: e.path("/projects"), token: e.owner.token, json: `{"description":"x"}`}, http.StatusBadRequest},
		{"blank name", metaReq{method: "POST", target: e.path("/projects"), token: e.owner.token, json: `{"name":"   "}`}, http.StatusBadRequest},
		{"unknown repo", metaReq{method: "POST", target: "/api/repos/" + e.owner.name + "/nope/projects", token: e.owner.token, json: `{"name":"x"}`}, http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantStatus(t, e.do(t, c.req), c.want) })
	}
	if n := e.count(t, `SELECT COUNT(*) FROM projects WHERE repo_id = $1`, e.repoID); n != 1 {
		t.Errorf("projects = %d, want 1", n)
	}

	testutil.Exec(t, e.db, `UPDATE repositories SET allow_projects = FALSE WHERE id = $1`, e.repoID)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/projects"), token: e.owner.token, json: `{"name":"y"}`}), http.StatusNotFound)
	if n := e.count(t, `SELECT COUNT(*) FROM projects WHERE repo_id = $1`, e.repoID); n != 1 {
		t.Error("a project was created while projects are disabled")
	}
}

func TestProjects_CloseAndDelete(t *testing.T) {
	e := newGitMetaEnv(t)
	id := e.createProject(t, "Board")
	target := e.path("/projects/%d", id)
	closed := func() bool {
		return e.count(t, `SELECT COUNT(*) FROM projects WHERE id = $1 AND closed_at IS NOT NULL`, id) == 1
	}

	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, json: `{"closed":true}`}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.outsider.token, json: `{"closed":true}`}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/projects/x"), token: e.owner.token, json: `{"closed":true}`}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/projects/999999999"), token: e.owner.token, json: `{"closed":true}`}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, json: `{`}), http.StatusBadRequest)
	if closed() {
		t.Fatal("refusals closed the project")
	}

	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.writer.token, json: `{"closed":true}`}), http.StatusOK)
	if !closed() {
		t.Error("project not closed")
	}
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, json: `{"closed":false}`}), http.StatusOK)
	if closed() {
		t.Error("project not reopened")
	}

	other := newGitMetaEnv(t)
	foreign := other.createProject(t, "Theirs")
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/projects/%d", foreign), token: e.owner.token, json: `{"closed":true}`}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/projects/%d", foreign), token: e.owner.token}), http.StatusNotFound)

	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target, token: e.writer.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target, token: e.outsider.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/projects/x"), token: e.owner.token}), http.StatusBadRequest)
	if n := e.count(t, `SELECT COUNT(*) FROM projects WHERE id IN ($1, $2)`, id, foreign); n != 2 {
		t.Fatalf("refusals deleted projects: %d left", n)
	}
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target, token: e.owner.token}), http.StatusNoContent)
	if n := e.count(t, `SELECT COUNT(*) FROM projects WHERE id = $1`, id); n != 0 {
		t.Error("project not deleted")
	}
	if n := e.count(t, `SELECT COUNT(*) FROM projects WHERE id = $1`, foreign); n != 1 {
		t.Error("another repo's project was deleted")
	}
}

func TestProjects_Columns(t *testing.T) {
	e := newGitMetaEnv(t)
	p := e.createProject(t, "Board")
	other := newGitMetaEnv(t)
	foreignProject := other.createProject(t, "Theirs")
	foreignCol := other.createColumn(t, foreignProject, "Their col")
	cols := e.path("/projects/%d/columns", p)

	cases := []struct {
		name string
		req  metaReq
		want int
	}{
		{"anonymous", metaReq{method: "POST", target: cols, json: `{"name":"x"}`}, http.StatusUnauthorized},
		{"outsider", metaReq{method: "POST", target: cols, token: e.outsider.token, json: `{"name":"x"}`}, http.StatusForbidden},
		{"bad project id", metaReq{method: "POST", target: e.path("/projects/x/columns"), token: e.owner.token, json: `{"name":"x"}`}, http.StatusBadRequest},
		{"unknown project", metaReq{method: "POST", target: e.path("/projects/999999999/columns"), token: e.owner.token, json: `{"name":"x"}`}, http.StatusNotFound},
		{"foreign project", metaReq{method: "POST", target: e.path("/projects/%d/columns", foreignProject), token: e.owner.token, json: `{"name":"x"}`}, http.StatusNotFound},
		{"bad json", metaReq{method: "POST", target: cols, token: e.owner.token, json: `{`}, http.StatusBadRequest},
		{"no name", metaReq{method: "POST", target: cols, token: e.owner.token, json: `{"name":""}`}, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantStatus(t, e.do(t, c.req), c.want) })
	}
	if n := e.count(t, `SELECT COUNT(*) FROM project_columns WHERE project_id = $1`, p); n != 0 {
		t.Fatalf("refusals created %d columns", n)
	}

	col := e.createColumn(t, p, "To do")
	del := e.path("/projects/%d/columns/%d", p, col)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: del}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: del, token: e.outsider.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/projects/%d/columns/x", p), token: e.owner.token}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/projects/%d/columns/%d", foreignProject, foreignCol), token: e.owner.token}), http.StatusNotFound)
	rr := e.do(t, metaReq{method: "DELETE", target: e.path("/projects/%d/columns/%d", p, foreignCol), token: e.owner.token})
	wantStatus(t, rr, http.StatusNotFound)
	bodyHas(t, rr, "project not found")
	if n := other.count(t, `SELECT COUNT(*) FROM project_columns WHERE id = $1`, foreignCol); n != 1 {
		t.Error("a column of another project was deleted through this project's URL")
	}
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: del, token: e.writer.token}), http.StatusNoContent)
	if n := e.count(t, `SELECT COUNT(*) FROM project_columns WHERE id = $1`, col); n != 0 {
		t.Error("column not deleted")
	}
}

func TestProjects_Cards(t *testing.T) {
	e := newGitMetaEnv(t)
	p := e.createProject(t, "Board")
	todo, done := e.createColumn(t, p, "To do"), e.createColumn(t, p, "Done")
	otherProject := e.createProject(t, "Other board")
	otherCol := e.createColumn(t, otherProject, "Elsewhere")
	issueID, _ := e.seedIssue(t, "carded issue", "open")
	pullID, _ := e.seedPull(t, "carded pull", "open")
	cards := e.path("/projects/%d/cards", p)

	bad := []struct {
		name string
		req  metaReq
		want int
	}{
		{"anonymous", metaReq{method: "POST", target: cards, json: fmt.Sprintf(`{"column_id":%d,"note":"x"}`, todo)}, http.StatusUnauthorized},
		{"outsider", metaReq{method: "POST", target: cards, token: e.outsider.token, json: fmt.Sprintf(`{"column_id":%d,"note":"x"}`, todo)}, http.StatusForbidden},
		{"bad json", metaReq{method: "POST", target: cards, token: e.owner.token, json: `{`}, http.StatusBadRequest},
		{"no column", metaReq{method: "POST", target: cards, token: e.owner.token, json: `{"note":"x"}`}, http.StatusBadRequest},
		{"empty card", metaReq{method: "POST", target: cards, token: e.owner.token, json: fmt.Sprintf(`{"column_id":%d,"note":"  "}`, todo)}, http.StatusBadRequest},
		{"column of another project", metaReq{method: "POST", target: cards, token: e.owner.token, json: fmt.Sprintf(`{"column_id":%d,"note":"x"}`, otherCol)}, http.StatusNotFound},
		{"unknown column", metaReq{method: "POST", target: cards, token: e.owner.token, json: `{"column_id":999999999,"note":"x"}`}, http.StatusNotFound},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) { wantStatus(t, e.do(t, c.req), c.want) })
	}
	if n := e.count(t, `SELECT COUNT(*) FROM project_cards WHERE column_id IN ($1, $2, $3)`, todo, done, otherCol); n != 0 {
		t.Fatalf("refusals created %d cards", n)
	}

	note := e.createNote(t, p, todo, "remember the milk")
	wantStatus(t, e.do(t, metaReq{method: "POST", target: cards, token: e.writer.token, json: fmt.Sprintf(`{"column_id":%d,"issue_id":%d}`, todo, issueID)}), http.StatusCreated)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: cards, token: e.owner.token, json: fmt.Sprintf(`{"column_id":%d,"pull_id":%d}`, done, pullID)}), http.StatusCreated)
	if n := e.count(t, `SELECT COUNT(*) FROM project_cards WHERE column_id IN ($1, $2)`, todo, done); n != 3 {
		t.Fatalf("cards = %d, want 3", n)
	}

	move := e.path("/projects/%d/cards/%d", p, note)
	moveBody := func(col int64, pos int) string { return fmt.Sprintf(`{"column_id":%d,"position":%d}`, col, pos) }
	moveBad := []struct {
		name string
		req  metaReq
		want int
	}{
		{"anonymous", metaReq{method: "PATCH", target: move, json: moveBody(done, 0)}, http.StatusUnauthorized},
		{"outsider", metaReq{method: "PATCH", target: move, token: e.outsider.token, json: moveBody(done, 0)}, http.StatusForbidden},
		{"bad card id", metaReq{method: "PATCH", target: e.path("/projects/%d/cards/x", p), token: e.owner.token, json: moveBody(done, 0)}, http.StatusBadRequest},
		{"bad json", metaReq{method: "PATCH", target: move, token: e.owner.token, json: `{`}, http.StatusBadRequest},
		{"no column", metaReq{method: "PATCH", target: move, token: e.owner.token, json: `{"position":1}`}, http.StatusBadRequest},
		{"negative position", metaReq{method: "PATCH", target: move, token: e.owner.token, json: moveBody(done, -1)}, http.StatusBadRequest},
		{"column of another project", metaReq{method: "PATCH", target: move, token: e.owner.token, json: moveBody(otherCol, 0)}, http.StatusNotFound},
		{"unknown card", metaReq{method: "PATCH", target: e.path("/projects/%d/cards/999999999", p), token: e.owner.token, json: moveBody(done, 0)}, http.StatusNotFound},
	}
	for _, c := range moveBad {
		t.Run("move "+c.name, func(t *testing.T) { wantStatus(t, e.do(t, c.req), c.want) })
	}
	colOf := func(card int64) int64 {
		var c int64
		if err := e.db.QueryRow(`SELECT column_id FROM project_cards WHERE id = $1`, card).Scan(&c); err != nil {
			t.Fatal(err)
		}
		return c
	}
	if colOf(note) != todo {
		t.Fatal("refused moves relocated the card")
	}
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: move, token: e.writer.token, json: moveBody(done, 0)}), http.StatusNoContent)
	if colOf(note) != done {
		t.Error("card not moved")
	}

	del := move
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: del}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: del, token: e.outsider.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/projects/%d/cards/x", p), token: e.owner.token}), http.StatusBadRequest)
	foreignCard := e.createNote(t, otherProject, otherCol, "theirs")
	rr := e.do(t, metaReq{method: "DELETE", target: e.path("/projects/%d/cards/%d", p, foreignCard), token: e.owner.token})
	wantStatus(t, rr, http.StatusNotFound)
	bodyHas(t, rr, "project not found")
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: e.path("/projects/%d/cards/999999999", p), token: e.owner.token}), http.StatusNotFound)
	if n := e.count(t, `SELECT COUNT(*) FROM project_cards WHERE id = $1`, foreignCard); n != 1 {
		t.Error("a card of another project was deleted through this project's URL")
	}
	if n := e.count(t, `SELECT COUNT(*) FROM project_cards WHERE id = $1`, note); n != 1 {
		t.Fatal("refusals deleted the card")
	}
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: del, token: e.writer.token}), http.StatusNoContent)
	if n := e.count(t, `SELECT COUNT(*) FROM project_cards WHERE id = $1`, note); n != 0 {
		t.Error("card not deleted")
	}
}

func TestProjects_CardDetails(t *testing.T) {
	e := newGitMetaEnv(t)
	p := e.createProject(t, "Board")
	col := e.createColumn(t, p, "To do")
	other := e.createProject(t, "Other board")
	note := e.createNote(t, p, col, "before")
	issueID, _ := e.seedIssue(t, "linked", "open")
	label := e.seedLabel(t, "bug")

	sfx := testutil.UniqueSuffix(t)
	foreignRepo := testutil.SeedRepo(t, e.db, e.owner.id, e.owner.name, sfx)
	var foreignLabel, foreignIssue int64
	if err := e.db.QueryRow(`INSERT INTO labels (repo_id, name) VALUES ($1, 'x') RETURNING id`, foreignRepo).Scan(&foreignLabel); err != nil {
		t.Fatal(err)
	}
	if err := e.db.QueryRow(`INSERT INTO issues (repo_id, number, author_id, title) VALUES ($1, 1, $2, 'f') RETURNING id`, foreignRepo, e.owner.id).Scan(&foreignIssue); err != nil {
		t.Fatal(err)
	}

	target := e.path("/projects/%d/cards/%d/details", p, note)
	body := `{"title":"after","description":"more"}`
	patch := func(token, path, json string) metaReq {
		return metaReq{method: "PATCH", target: path, token: token, json: json}
	}
	cases := []struct {
		name string
		req  metaReq
		want int
	}{
		{"anonymous", patch("", target, body), http.StatusUnauthorized},
		{"outsider", patch(e.outsider.token, target, body), http.StatusForbidden},
		{"bad json", patch(e.writer.token, target, `{`), http.StatusBadRequest},
		{"blank title on a note card", patch(e.writer.token, target, `{"title":"  "}`), http.StatusBadRequest},
		{"assignee outside the repo", patch(e.writer.token, target, fmt.Sprintf(`{"title":"t","assignee_ids":[%d]}`, e.outsider.id)), http.StatusBadRequest},
		{"label of another repo", patch(e.writer.token, target, fmt.Sprintf(`{"title":"t","label_ids":[%d]}`, foreignLabel)), http.StatusBadRequest},
		{"issue of another repo", patch(e.writer.token, target, fmt.Sprintf(`{"title":"t","issue_id":%d}`, foreignIssue)), http.StatusNotFound},
		{"card of another project", patch(e.writer.token, e.path("/projects/%d/cards/%d/details", other, note), body), http.StatusNotFound},
		{"bad due date", patch(e.writer.token, target, `{"title":"t","due_date":"31/12"}`), http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantStatus(t, e.do(t, c.req), c.want) })
	}
	cardTitle := func() string {
		var s string
		if err := e.db.QueryRow(`SELECT title FROM project_cards WHERE id = $1`, note).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	if got := cardTitle(); got != "before" {
		t.Fatalf("refusals changed the title to %q", got)
	}

	full := fmt.Sprintf(`{"title":"after","description":"more","due_date":"2030-01-02","assignee_ids":[%d],"label_ids":[%d],"issue_id":%d}`,
		e.writer.id, label, issueID)
	wantStatus(t, e.do(t, patch(e.writer.token, target, full)), http.StatusNoContent)
	var title, desc, due string
	var linked int64
	if err := e.db.QueryRow(`SELECT title, note, due_date::text, issue_id FROM project_cards WHERE id = $1`, note).Scan(&title, &desc, &due, &linked); err != nil {
		t.Fatal(err)
	}
	if title != "after" || desc != "more" || due != "2030-01-02" || linked != issueID {
		t.Errorf("card = %q %q %q %d", title, desc, due, linked)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM card_assignees WHERE card_id = $1 AND user_id = $2`, note, e.writer.id); n != 1 {
		t.Errorf("assignee rows = %d, want 1", n)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM card_labels WHERE card_id = $1 AND label_id = $2`, note, label); n != 1 {
		t.Errorf("label rows = %d, want 1", n)
	}

	wantStatus(t, e.do(t, patch(e.writer.token, target, `{"title":"after","assignee_ids":[],"label_ids":[]}`)), http.StatusNoContent)
	if n := e.count(t, `SELECT (SELECT COUNT(*) FROM card_assignees WHERE card_id = $1) + (SELECT COUNT(*) FROM card_labels WHERE card_id = $1)`, note); n != 0 {
		t.Errorf("join rows = %d, want 0", n)
	}

	cards := e.path("/projects/%d/cards", p)
	post := func(json string) *httptest.ResponseRecorder {
		return e.do(t, metaReq{method: "POST", target: cards, token: e.writer.token, json: json})
	}
	wantStatus(t, post(fmt.Sprintf(`{"column_id":%d,"title":"x"}`, col)), http.StatusCreated)
	wantStatus(t, post(fmt.Sprintf(`{"column_id":%d,"issue_id":%d}`, col, issueID)), http.StatusCreated)
	wantStatus(t, post(fmt.Sprintf(`{"column_id":%d}`, col)), http.StatusBadRequest)

	rr := post(fmt.Sprintf(`{"column_id":%d,"title":"full","description":"desc","assignee_ids":[%d],"label_ids":[%d]}`, col, e.writer.id, label))
	wantStatus(t, rr, http.StatusCreated)
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM project_cards WHERE id = $1 AND title = 'full' AND note = 'desc'`, created.ID); n != 1 {
		t.Errorf("created card rows = %d, want 1", n)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM card_assignees WHERE card_id = $1 AND user_id = $2`, created.ID, e.writer.id); n != 1 {
		t.Errorf("assignee rows = %d, want 1", n)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM card_labels WHERE card_id = $1 AND label_id = $2`, created.ID, label); n != 1 {
		t.Errorf("label rows = %d, want 1", n)
	}

	rr = post(fmt.Sprintf(`{"column_id":%d,"issue_id":%d}`, col, issueID))
	wantStatus(t, rr, http.StatusCreated)
	var linkedCard struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &linkedCard); err != nil {
		t.Fatal(err)
	}
	linkedTarget := e.path("/projects/%d/cards/%d/details", p, linkedCard.ID)
	wantStatus(t, e.do(t, patch(e.writer.token, linkedTarget, fmt.Sprintf(`{"title":"","issue_id":%d}`, issueID))), http.StatusNoContent)
	wantStatus(t, e.do(t, patch(e.writer.token, linkedTarget, fmt.Sprintf(`{"title":"named","issue_id":%d}`, issueID))), http.StatusNoContent)
	wantStatus(t, e.do(t, patch(e.writer.token, linkedTarget, fmt.Sprintf(`{"title":" ","issue_id":%d}`, issueID))), http.StatusBadRequest)
}

func TestProjects_CardDetailsKeepsAssigneeRemovedFromRepo(t *testing.T) {
	e := newGitMetaEnv(t)
	p := e.createProject(t, "Board")
	col := e.createColumn(t, p, "To do")
	card := e.createNote(t, p, col, "assigned")
	target := e.path("/projects/%d/cards/%d/details", p, card)
	patch := func(assignees string) *httptest.ResponseRecorder {
		return e.do(t, metaReq{method: "PATCH", target: target, token: e.owner.token, json: `{"title":"assigned","assignee_ids":` + assignees + `}`})
	}

	wantStatus(t, patch(fmt.Sprintf(`[%d]`, e.writer.id)), http.StatusNoContent)
	testutil.Exec(t, e.db, `DELETE FROM permissions WHERE repo_id = $1 AND user_id = $2`, e.repoID, e.writer.id)

	wantStatus(t, patch(fmt.Sprintf(`[%d]`, e.writer.id)), http.StatusNoContent)
	wantStatus(t, patch(fmt.Sprintf(`[%d,%d]`, e.writer.id, e.outsider.id)), http.StatusBadRequest)
	if n := e.count(t, `SELECT COUNT(*) FROM card_assignees WHERE card_id = $1 AND user_id = $2`, card, e.writer.id); n != 1 {
		t.Fatalf("refused save changed the assignee rows: %d, want 1", n)
	}
	wantStatus(t, patch(`[]`), http.StatusNoContent)
	if n := e.count(t, `SELECT COUNT(*) FROM card_assignees WHERE card_id = $1`, card); n != 0 {
		t.Errorf("assignee rows after removal = %d, want 0", n)
	}
}

func TestProjects_CardTargets(t *testing.T) {
	e := newGitMetaEnv(t)
	p := e.createProject(t, "Board")
	_, issueNum := e.seedIssue(t, "Fix login Crash", "open")
	e.seedPull(t, "Add 100%_done flag", "open")
	e.seedIssue(t, "unrelated", "closed")
	target := func(q string) string { return e.path("/projects/%d/card-targets?q=%s", p, q) }

	wantStatus(t, e.do(t, metaReq{method: "GET", target: target("x")}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: target("x"), token: e.outsider.token}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/projects/999999999/card-targets"), token: e.owner.token}), http.StatusNotFound)

	search := func(q string) []string {
		rr := e.do(t, metaReq{method: "GET", target: target(q), token: e.writer.token})
		wantStatus(t, rr, http.StatusOK)
		var got []struct {
			Kind  string `json:"kind"`
			Title string `json:"title"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, g := range got {
			out = append(out, g.Kind+":"+g.Title)
		}
		return out
	}
	if got := search("crash"); len(got) != 1 || got[0] != "issue:Fix login Crash" {
		t.Errorf("title match = %v", got)
	}
	if got := search(fmt.Sprintf("%%23%d", issueNum)); !slices.Contains(got, "issue:Fix login Crash") {
		t.Errorf("#number match = %v, want it to contain issue:Fix login Crash", got)
	}
	rr := e.do(t, metaReq{method: "GET", target: target("%2399999999999"), token: e.writer.token})
	wantStatus(t, rr, http.StatusOK)
	if body := strings.TrimSpace(rr.Body.String()); body != "[]" {
		t.Errorf("out-of-range #number body = %q, want []", body)
	}
	if got := search("%25_"); len(got) != 1 || got[0] != "pull:Add 100%_done flag" {
		t.Errorf("literal wildcard match = %v", got)
	}
	if got := search(""); len(got) != 3 {
		t.Errorf("empty query = %v, want all 3", got)
	}
}

func TestProjects_Pages(t *testing.T) {
	e := newGitMetaEnv(t)
	open := e.createProject(t, "Alpha board")
	closedID := e.createProject(t, "Beta board")
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/projects/%d", closedID), token: e.owner.token, json: `{"closed":true}`}), http.StatusOK)
	col := e.createColumn(t, open, "Backlog")
	e.createNote(t, open, col, "write the docs")
	issueID, _ := e.seedIssue(t, "board issue", "open")
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/projects/%d/cards", open), token: e.owner.token, json: fmt.Sprintf(`{"column_id":%d,"issue_id":%d}`, col, issueID)}), http.StatusCreated)

	rr := e.page(t, e.pagePath("/projects"), "", false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "Alpha board")
	if got := rr.Body.String(); strings.Contains(got, "Beta board") {
		t.Error("the open tab lists a closed board")
	}
	rr = e.page(t, e.pagePath("/projects?state=closed"), e.owner.token, false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "Beta board")
	rr = e.page(t, e.pagePath("/projects?q=alph"), "", false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "Alpha board")
	rr = e.page(t, e.pagePath("/projects?state=bogus&q=zzz-no-match"), "", false)
	wantStatus(t, rr, http.StatusOK)
	if strings.Contains(rr.Body.String(), "Alpha board") {
		t.Error("a non-matching query still lists boards")
	}

	rr = e.page(t, e.pagePath("/projects/%d", open), "", false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "Backlog")
	bodyHas(t, rr, "write the docs")
	bodyHas(t, rr, "board issue")
	wantStatus(t, e.page(t, e.pagePath("/projects/%d", open), e.writer.token, false), http.StatusOK)
	wantStatus(t, e.page(t, e.pagePath("/projects/x"), "", false), http.StatusBadRequest)
	wantStatus(t, e.page(t, e.pagePath("/projects/999999999"), "", false), http.StatusNotFound)
	wantStatus(t, e.page(t, "/"+e.owner.name+"/nope/projects", "", false), http.StatusNotFound)

	other := newGitMetaEnv(t)
	foreign := other.createProject(t, "Theirs")
	wantStatus(t, e.page(t, e.pagePath("/projects/%d", foreign), "", false), http.StatusNotFound)

	testutil.Exec(t, e.db, `UPDATE repositories SET allow_projects = FALSE WHERE id = $1`, e.repoID)
	wantStatus(t, e.page(t, e.pagePath("/projects"), "", false), http.StatusNotFound)
	wantStatus(t, e.page(t, e.pagePath("/projects/%d", open), "", false), http.StatusNotFound)
	testutil.Exec(t, e.db, `UPDATE repositories SET allow_projects = TRUE, private = TRUE WHERE id = $1`, e.repoID)
	wantStatus(t, e.page(t, e.pagePath("/projects"), e.outsider.token, false), http.StatusNotFound)
	wantStatus(t, e.page(t, e.pagePath("/projects/%d", open), e.outsider.token, false), http.StatusNotFound)
}

func TestProjects_BoardPanelData(t *testing.T) {
	e := newGitMetaEnv(t)
	board := e.createProject(t, "Panel board")
	col := e.createColumn(t, board, "Todo")
	e.createNote(t, board, col, "panel card")
	e.seedLabel(t, "triage-me")

	rr := e.page(t, e.pagePath("/projects/%d", board), e.writer.token, false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "data-panel-people")
	bodyHas(t, rr, fmt.Sprintf(`x-model="panel.assignees" value="%d"`, e.owner.id))
	bodyHas(t, rr, fmt.Sprintf(`x-model="panel.assignees" value="%d"`, e.writer.id))
	bodyHas(t, rr, "data-panel-labels")
	bodyHas(t, rr, "triage-me")
	bodyHas(t, rr, `@click="savePanel()"`)
	bodyHas(t, rr, "cardComposer(")

	rr = e.page(t, e.pagePath("/projects/%d", board), "", false)
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "panel card")
	bodyHas(t, rr, `id="card-panel-title-input"`)
	for _, gone := range []string{"data-panel-people", "data-panel-labels", "triage-me", "savePanel", "cardComposer(", "linkPicker"} {
		if strings.Contains(rr.Body.String(), gone) {
			t.Errorf("anonymous board renders %q", gone)
		}
	}
}
