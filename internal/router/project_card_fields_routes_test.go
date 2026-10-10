package router_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// cardState is everything a partial update may change, read straight from the database.
type cardState struct {
	title, note, due string
	issue, pull      int64
	assignees        []int64
	labels           []int64
}

func (e metaEnv) cardState(t *testing.T, card int64) cardState {
	t.Helper()
	var s cardState
	if err := e.db.QueryRow(`SELECT title, note, COALESCE(due_date::text, ''), COALESCE(issue_id, 0), COALESCE(pull_id, 0)
		FROM project_cards WHERE id = $1`, card).Scan(&s.title, &s.note, &s.due, &s.issue, &s.pull); err != nil {
		t.Fatal(err)
	}
	ids := func(query string) []int64 {
		rows, err := e.db.Query(query, card)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := []int64{}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			out = append(out, id)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	s.assignees = ids(`SELECT user_id FROM card_assignees WHERE card_id = $1 ORDER BY user_id`)
	s.labels = ids(`SELECT label_id FROM card_labels WHERE card_id = $1 ORDER BY label_id`)
	return s
}

func (s cardState) String() string {
	return fmt.Sprintf("{title:%q note:%q due:%q issue:%d pull:%d assignees:%v labels:%v}",
		s.title, s.note, s.due, s.issue, s.pull, s.assignees, s.labels)
}

func sameCard(a, b cardState) bool {
	return a.title == b.title && a.note == b.note && a.due == b.due && a.issue == b.issue && a.pull == b.pull &&
		slices.Equal(a.assignees, b.assignees) && slices.Equal(a.labels, b.labels)
}

type fieldsEnv struct {
	metaEnv
	project, column, card int64
	issueID, pullID       int64
	issueNum              int
	label, label2         int64
}

// newFieldsEnv seeds a titled card with every field set.
func newFieldsEnv(t *testing.T) fieldsEnv {
	t.Helper()
	e := fieldsEnv{metaEnv: newGitMetaEnv(t)}
	e.project = e.createProject(t, "Board")
	e.column = e.createColumn(t, e.project, "To do")
	e.card = e.createNote(t, e.project, e.column, "before")
	e.issueID, e.issueNum = e.seedIssue(t, "linked", "open")
	e.pullID, _ = e.seedPull(t, "pulled", "open")
	e.label, e.label2 = e.seedLabel(t, "bug"), e.seedLabel(t, "docs")
	full := fmt.Sprintf(`{"title":"before","description":"desc","due_date":"2030-01-02","assignee_ids":[%d],"label_ids":[%d],"issue_id":%d}`,
		e.writer.id, e.label, e.issueID)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/projects/%d/cards/%d/details", e.project, e.card), token: e.writer.token, json: full}), http.StatusNoContent)
	return e
}

func (e fieldsEnv) fields(card int64) string {
	return e.path("/projects/%d/cards/%d/fields", e.project, card)
}

func (e fieldsEnv) patchFields(t *testing.T, token, json string) (int, string) {
	t.Helper()
	rr := e.do(t, metaReq{method: "PATCH", target: e.fields(e.card), token: token, json: json})
	return rr.Code, rr.Body.String()
}

func TestProjects_CardFieldsRefusals(t *testing.T) {
	e := newFieldsEnv(t)
	other := e.createProject(t, "Other board")
	before := e.cardState(t, e.card)

	cases := []struct {
		name, token, target, json string
		want                      int
	}{
		{"anonymous", "", e.fields(e.card), `{"title":"x"}`, http.StatusUnauthorized},
		{"read-only outsider", e.outsider.token, e.fields(e.card), `{"title":"x"}`, http.StatusForbidden},
		{"card of another project", e.writer.token, e.path("/projects/%d/cards/%d/fields", other, e.card), `{"title":"x"}`, http.StatusNotFound},
		{"unknown card", e.writer.token, e.fields(999999999), `{"title":"x"}`, http.StatusNotFound},
		{"bad card id", e.writer.token, e.path("/projects/%d/cards/x/fields", e.project), `{"title":"x"}`, http.StatusBadRequest},
		{"bad json", e.writer.token, e.fields(e.card), `{`, http.StatusBadRequest},
		{"empty body", e.writer.token, e.fields(e.card), ` `, http.StatusBadRequest},
		{"no fields", e.writer.token, e.fields(e.card), `{}`, http.StatusBadRequest},
		{"null body", e.writer.token, e.fields(e.card), `null`, http.StatusBadRequest},
		{"array body", e.writer.token, e.fields(e.card), `[]`, http.StatusBadRequest},
		{"unknown key", e.writer.token, e.fields(e.card), `{"title":"x","note":"y"}`, http.StatusBadRequest},
		{"title not a string", e.writer.token, e.fields(e.card), `{"title":5}`, http.StatusBadRequest},
		{"null title", e.writer.token, e.fields(e.card), `{"title":null}`, http.StatusBadRequest},
		{"null description", e.writer.token, e.fields(e.card), `{"description":null}`, http.StatusBadRequest},
		{"assignees not a list", e.writer.token, e.fields(e.card), `{"assignee_ids":"1"}`, http.StatusBadRequest},
		{"null assignees", e.writer.token, e.fields(e.card), `{"assignee_ids":null}`, http.StatusBadRequest},
		{"null labels", e.writer.token, e.fields(e.card), `{"label_ids":null}`, http.StatusBadRequest},
		{"issue id not a number", e.writer.token, e.fields(e.card), `{"issue_id":"1"}`, http.StatusBadRequest},
		{"due date not a string", e.writer.token, e.fields(e.card), `{"due_date":20300102}`, http.StatusBadRequest},
		{"bad due date", e.writer.token, e.fields(e.card), `{"due_date":"31/12"}`, http.StatusBadRequest},
		{"due date before year 1", e.writer.token, e.fields(e.card), `{"due_date":"0000-01-01"}`, http.StatusBadRequest},
		{"blank title on a titled card", e.writer.token, e.fields(e.card), `{"title":"   "}`, http.StatusBadRequest},
		{"assignee outside the repo", e.writer.token, e.fields(e.card), fmt.Sprintf(`{"assignee_ids":[%d]}`, e.outsider.id), http.StatusBadRequest},
		{"too many labels", e.writer.token, e.fields(e.card), `{"label_ids":[` + strings.Repeat("1,", 50) + `1]}`, http.StatusBadRequest},
		{"oversized description", e.writer.token, e.fields(e.card), `{"description":"` + strings.Repeat("a", 65537) + `"}`, http.StatusBadRequest},
		{"second link alongside the first", e.writer.token, e.fields(e.card), fmt.Sprintf(`{"pull_id":%d}`, e.pullID), http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wantStatus(t, e.do(t, metaReq{method: "PATCH", target: c.target, token: c.token, json: c.json}), c.want)
		})
	}

	sfx := testutil.UniqueSuffix(t)
	foreignRepo := testutil.SeedRepo(t, e.db, e.owner.id, e.owner.name, sfx)
	var foreignLabel, foreignIssue int64
	if err := e.db.QueryRow(`INSERT INTO labels (repo_id, name) VALUES ($1, 'x') RETURNING id`, foreignRepo).Scan(&foreignLabel); err != nil {
		t.Fatal(err)
	}
	if err := e.db.QueryRow(`INSERT INTO issues (repo_id, number, author_id, title) VALUES ($1, 1, $2, 'f') RETURNING id`, foreignRepo, e.owner.id).Scan(&foreignIssue); err != nil {
		t.Fatal(err)
	}
	if code, body := e.patchFields(t, e.writer.token, fmt.Sprintf(`{"label_ids":[%d]}`, foreignLabel)); code != http.StatusBadRequest {
		t.Errorf("label of another repo = %d %s, want 400", code, body)
	}
	if code, body := e.patchFields(t, e.writer.token, fmt.Sprintf(`{"issue_id":%d}`, foreignIssue)); code != http.StatusNotFound {
		t.Errorf("issue of another repo = %d %s, want 404", code, body)
	}

	if after := e.cardState(t, e.card); !sameCard(before, after) {
		t.Fatalf("refusals changed the card:\nbefore %v\nafter  %v", before, after)
	}
}

func TestProjects_CardFieldsEachKeyAlone(t *testing.T) {
	e := newFieldsEnv(t)
	steps := []struct {
		name, json string
		change     func(*cardState)
	}{
		{"title", `{"title":"  after  "}`, func(s *cardState) { s.title = "after" }},
		{"description", `{"description":"see #1"}`, func(s *cardState) { s.note = "see #1" }},
		{"due date", `{"due_date":"2031-05-06"}`, func(s *cardState) { s.due = "2031-05-06" }},
		{"due date null clears", `{"due_date":null}`, func(s *cardState) { s.due = "" }},
		{"due date again", `{"due_date":"2032-02-29"}`, func(s *cardState) { s.due = "2032-02-29" }},
		{"due date empty clears", `{"due_date":""}`, func(s *cardState) { s.due = "" }},
		{"assignees", fmt.Sprintf(`{"assignee_ids":[%d,%d]}`, e.owner.id, e.writer.id), func(s *cardState) {
			s.assignees = []int64{e.owner.id, e.writer.id}
			slices.Sort(s.assignees)
		}},
		{"assignees empty clears", `{"assignee_ids":[]}`, func(s *cardState) { s.assignees = []int64{} }},
		{"labels", fmt.Sprintf(`{"label_ids":[%d]}`, e.label2), func(s *cardState) { s.labels = []int64{e.label2} }},
		{"labels empty clears", `{"label_ids":[]}`, func(s *cardState) { s.labels = []int64{} }},
		{"unlink", `{"issue_id":null}`, func(s *cardState) { s.issue = 0 }},
		{"link a pull", fmt.Sprintf(`{"pull_id":%d}`, e.pullID), func(s *cardState) { s.pull = e.pullID }},
		{"swap to an issue", fmt.Sprintf(`{"issue_id":%d,"pull_id":null}`, e.issueID), func(s *cardState) { s.issue, s.pull = e.issueID, 0 }},
	}
	for _, step := range steps {
		want := e.cardState(t, e.card)
		step.change(&want)
		code, body := e.patchFields(t, e.writer.token, step.json)
		if code != http.StatusOK {
			t.Fatalf("%s: status %d, body %s", step.name, code, body)
		}
		if got := e.cardState(t, e.card); !sameCard(got, want) {
			t.Errorf("%s:\n got  %v\n want %v", step.name, got, want)
		}
	}
}

func TestProjects_CardFieldsResponse(t *testing.T) {
	e := newFieldsEnv(t)
	code, body := e.patchFields(t, e.writer.token, fmt.Sprintf(`{"description":"fixes #%d","assignee_ids":[%d],"label_ids":[%d]}`, e.issueNum, e.writer.id, e.label))
	if code != http.StatusOK {
		t.Fatalf("status %d, body %s", code, body)
	}
	var got struct {
		ID              int64  `json:"id"`
		Title           string `json:"title"`
		Note            string `json:"note"`
		DueDate         string `json:"due_date"`
		IssueID         int64  `json:"issue_id"`
		IssueNumber     int    `json:"issue_number"`
		IssueTitle      string `json:"issue_title"`
		IssueState      string `json:"issue_state"`
		DescriptionHTML string `json:"description_html"`
		Assignees       []struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
		} `json:"assignees"`
		Labels []struct {
			ID    int64  `json:"id"`
			Name  string `json:"name"`
			Color string `json:"color"`
		} `json:"labels"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != e.card || got.Title != "before" || got.Note != fmt.Sprintf("fixes #%d", e.issueNum) || !strings.HasPrefix(got.DueDate, "2030-01-02") {
		t.Errorf("card fields = %+v", got)
	}
	if got.IssueID != e.issueID || got.IssueNumber != e.issueNum || got.IssueTitle != "linked" || got.IssueState != "open" {
		t.Errorf("link = %d #%d %q %q", got.IssueID, got.IssueNumber, got.IssueTitle, got.IssueState)
	}
	wantLink := fmt.Sprintf(`href="/%s/%s/issues/%d"`, e.owner.name, e.repoName, e.issueNum)
	if !strings.Contains(got.DescriptionHTML, wantLink) {
		t.Errorf("description_html = %q, want a link %s", got.DescriptionHTML, wantLink)
	}
	if len(got.Assignees) != 1 || got.Assignees[0].ID != e.writer.id || got.Assignees[0].Username != e.writer.name {
		t.Errorf("assignees = %+v", got.Assignees)
	}
	if len(got.Labels) != 1 || got.Labels[0].ID != e.label || got.Labels[0].Name != "bug" {
		t.Errorf("labels = %+v", got.Labels)
	}

	code, body = e.patchFields(t, e.writer.token, `{"description":"","assignee_ids":[],"label_ids":[]}`)
	if code != http.StatusOK {
		t.Fatalf("clear: status %d, body %s", code, body)
	}
	for _, want := range []string{`"description_html":""`, `"assignees":[]`, `"labels":[]`} {
		if !strings.Contains(body, want) {
			t.Errorf("cleared response lacks %s: %s", want, body)
		}
	}
}

func TestProjects_CardFieldsTitleLessLinkedCard(t *testing.T) {
	e := newFieldsEnv(t)
	rr := e.do(t, metaReq{method: "POST", target: e.path("/projects/%d/cards", e.project), token: e.writer.token,
		json: fmt.Sprintf(`{"column_id":%d,"issue_id":%d}`, e.column, e.issueID)})
	wantStatus(t, rr, http.StatusCreated)
	var c struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	patch := func(json string) int {
		return e.do(t, metaReq{method: "PATCH", target: e.fields(c.ID), token: e.writer.token, json: json}).Code
	}
	if code := patch(fmt.Sprintf(`{"label_ids":[%d]}`, e.label)); code != http.StatusOK {
		t.Errorf("label a title-less linked card = %d, want 200", code)
	}
	if code := patch(`{"issue_id":null}`); code != http.StatusBadRequest {
		t.Errorf("unlink a title-less card = %d, want 400", code)
	}
	if s := e.cardState(t, c.ID); s.issue != e.issueID || s.title != "" {
		t.Errorf("card after refused unlink = %v", s)
	}
}

func TestProjects_CardFieldsKeepsAssigneeRemovedFromRepo(t *testing.T) {
	e := newFieldsEnv(t)
	testutil.Exec(t, e.db, `DELETE FROM permissions WHERE repo_id = $1 AND user_id = $2`, e.repoID, e.writer.id)

	if code, body := e.patchFields(t, e.owner.token, fmt.Sprintf(`{"label_ids":[%d]}`, e.label2)); code != http.StatusOK {
		t.Fatalf("saving labels next to a stale assignee = %d %s", code, body)
	}
	if code, _ := e.patchFields(t, e.owner.token, fmt.Sprintf(`{"assignee_ids":[%d]}`, e.writer.id)); code != http.StatusOK {
		t.Errorf("keeping a stale assignee = %d, want 200", code)
	}
	if code, _ := e.patchFields(t, e.owner.token, fmt.Sprintf(`{"assignee_ids":[%d,%d]}`, e.writer.id, e.outsider.id)); code != http.StatusBadRequest {
		t.Errorf("adding an outsider = %d, want 400", code)
	}
	if s := e.cardState(t, e.card); !slices.Equal(s.assignees, []int64{e.writer.id}) {
		t.Errorf("assignees = %v, want the stale one kept", s.assignees)
	}
}

// Two fields saved at once must both survive: each save merges into the card as stored under its row lock.
func TestProjects_CardFieldsConcurrentSavesBothSurvive(t *testing.T) {
	e := newFieldsEnv(t)
	for i := range 10 {
		title := fmt.Sprintf(`{"title":"title %d"}`, i)
		desc := fmt.Sprintf(`{"description":"desc %d"}`, i)
		labels := fmt.Sprintf(`{"label_ids":[%d,%d]}`, e.label, e.label2)
		if i%2 == 1 {
			labels = `{"label_ids":[]}`
		}
		codes := make([]int, 3)
		var wg sync.WaitGroup
		for j, body := range []string{title, desc, labels} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				codes[j] = e.do(t, metaReq{method: "PATCH", target: e.fields(e.card), token: e.writer.token, json: body}).Code
			}()
		}
		wg.Wait()
		for j, code := range codes {
			if code != http.StatusOK {
				t.Fatalf("round %d save %d: status %d", i, j, code)
			}
		}
		s := e.cardState(t, e.card)
		wantLabels := []int64{e.label, e.label2}
		if i%2 == 1 {
			wantLabels = []int64{}
		}
		slices.Sort(wantLabels)
		if s.title != fmt.Sprintf("title %d", i) || s.note != fmt.Sprintf("desc %d", i) || !slices.Equal(s.labels, wantLabels) {
			t.Fatalf("round %d lost a save: %v", i, s)
		}
	}
}
