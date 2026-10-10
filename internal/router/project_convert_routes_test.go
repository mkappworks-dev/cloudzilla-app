package router_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProjects_ConvertCardAnnouncesTheNewIssue(t *testing.T) {
	e := newGitMetaEnv(t)
	hook := e.seedHook(t, "issues")
	p := e.createProject(t, "Board")
	col := e.createColumn(t, p, "To do")
	card := e.createNote(t, p, col, "Announce me")

	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/projects/%d/cards/%d/convert", p, card), token: e.writer.token}), http.StatusOK)

	waitFor(t, "issue_opened event", func() bool {
		return e.count(t, `SELECT COUNT(*) FROM events WHERE repo_id = $1 AND actor_id = $2 AND event_type = 'issue_opened' AND payload->>'title' = 'Announce me'`, e.repoID, e.writer.id) == 1
	})
	waitFor(t, "issues webhook delivery", func() bool {
		return e.count(t, `SELECT COUNT(*) FROM webhook_deliveries WHERE webhook_id = $1 AND event = 'issues' AND payload LIKE '%"opened"%'`, hook) == 1
	})
}

func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestProjects_ConvertCard(t *testing.T) {
	e := newGitMetaEnv(t)
	p := e.createProject(t, "Board")
	other := e.createProject(t, "Other board")
	col := e.createColumn(t, p, "To do")
	card := e.createNote(t, p, col, "Ship it")
	label := e.seedLabel(t, "bug")
	issueID, _ := e.seedIssue(t, "existing", "open")

	details := e.path("/projects/%d/cards/%d/details", p, card)
	body := fmt.Sprintf(`{"title":"Ship it","description":"the body","due_date":"2030-01-02","assignee_ids":[%d],"label_ids":[%d]}`, e.writer.id, label)
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: details, token: e.writer.token, json: body}), http.StatusNoContent)
	linked := e.createNote(t, p, col, "Already linked")
	wantStatus(t, e.do(t, metaReq{method: "PATCH", target: e.path("/projects/%d/cards/%d/details", p, linked), token: e.writer.token,
		json: fmt.Sprintf(`{"title":"Already linked","issue_id":%d}`, issueID)}), http.StatusNoContent)

	convert := func(token string, project, id int64) *httptest.ResponseRecorder {
		return e.do(t, metaReq{method: "POST", target: e.path("/projects/%d/cards/%d/convert", project, id), token: token})
	}
	wantStatus(t, convert("", p, card), http.StatusUnauthorized)
	wantStatus(t, convert(e.outsider.token, p, card), http.StatusForbidden)
	wantStatus(t, convert(e.writer.token, p, linked), http.StatusBadRequest)
	wantStatus(t, convert(e.writer.token, other, card), http.StatusNotFound)
	if n := e.issueCount(t); n != 1 {
		t.Fatalf("refusals left %d issues, want 1", n)
	}

	rr := convert(e.writer.token, p, card)
	wantStatus(t, rr, http.StatusOK)
	var got struct {
		IssueID     *int64 `json:"issue_id"`
		IssueNumber int    `json:"issue_number"`
		IssueTitle  string `json:"issue_title"`
		IssueState  string `json:"issue_state"`
		Title       string `json:"title"`
		Note        string `json:"note"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.IssueID == nil || got.Title != "Ship it" || got.Note != "the body" {
		t.Fatalf("response card = %s, want it linked and still titled", rr.Body.String())
	}
	var number int
	if err := e.db.QueryRow(`SELECT number FROM issues WHERE id = $1`, *got.IssueID).Scan(&number); err != nil {
		t.Fatal(err)
	}
	if got.IssueNumber != number || got.IssueNumber == 0 || got.IssueTitle != "Ship it" || got.IssueState != "open" {
		t.Errorf("response card = %s, want issue_number %d, issue_title and an open issue_state for the modal's link chip", rr.Body.String(), number)
	}
	var due string
	if err := e.db.QueryRow(`SELECT due_date::text FROM project_cards WHERE id = $1`, card).Scan(&due); err != nil || due != "2030-01-02" {
		t.Errorf("due date = %q (%v), want kept", due, err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM issues WHERE id = $1 AND repo_id = $2 AND title = 'Ship it' AND body = 'the body' AND visibility = 'public' AND author_id = $3`,
		*got.IssueID, e.repoID, e.writer.id); n != 1 {
		t.Errorf("matching issues = %d, want 1", n)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM issue_labels WHERE issue_id = $1 AND label_id = $2`, *got.IssueID, label); n != 1 {
		t.Errorf("issue label rows = %d, want 1", n)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM issue_assignees WHERE issue_id = $1 AND user_id = $2`, *got.IssueID, e.writer.id); n != 1 {
		t.Errorf("issue assignee rows = %d, want 1", n)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM card_assignees WHERE card_id = $1 AND user_id = $2`, card, e.writer.id); n != 1 {
		t.Errorf("card assignee rows = %d, want the card to keep its own", n)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM card_labels WHERE card_id = $1 AND label_id = $2`, card, label); n != 1 {
		t.Errorf("card label rows = %d, want the card to keep its own", n)
	}
	wantStatus(t, convert(e.writer.token, p, card), http.StatusBadRequest)
}
