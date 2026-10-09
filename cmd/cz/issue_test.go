package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	openIssue   = `{"number":1,"title":"Crash on start","body":"It dies.","state":"open","author_name":"ada","created_at":"2026-10-01T00:00:00Z"}`
	closedIssue = `{"number":2,"title":"Old bug","body":"","state":"closed","author_name":"bob","created_at":"2026-09-01T00:00:00Z"}`
)

func TestIssueListFiltersStateAndRendersTable(t *testing.T) {
	h := newRepoHarness(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /api/repos/ada/demo/issues/": reply(200, `[`+openIssue+`,`+closedIssue+`]`),
	})
	if err := h.run("issue", "list", "-R", "ada/demo", "--state", "closed"); err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal(h.out.Bytes(), &got); err != nil || len(got) != 1 || got[0]["number"] != float64(2) {
		t.Fatalf("piped output = %s (%v)", h.out.String(), err)
	}

	h.app.stdoutTTY = true
	if err := h.run("issue", "list", "-R", "ada/demo"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"NUMBER", "#1", "Crash on start", "#2", "closed"} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("table lacks %q:\n%s", want, h.out.String())
		}
	}
	if err := h.run("issue", "list", "-R", "ada/demo", "--state", "bogus"); err == nil {
		t.Error("an unknown state should fail")
	}
}

func TestIssueListUsesOriginRemoteWhenNoFlag(t *testing.T) {
	h := newRepoHarness(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /api/repos/ada/demo/issues/": reply(200, `[]`),
	})
	h.app.getwd = func() (string, error) { return remoteCheckout(t, "git@"+hostOnly(h.api.URL)+":ada/demo.git"), nil }
	if err := h.run("issue", "list"); err != nil {
		t.Fatal(err)
	}
	if len(h.reqs) != 1 || h.reqs[0] != "GET /api/repos/ada/demo/issues/" {
		t.Errorf("requests = %v", h.reqs)
	}
}

func TestIssueViewIncludesComments(t *testing.T) {
	h := newRepoHarness(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /api/repos/ada/demo/issues/1":          reply(200, openIssue),
		"GET /api/repos/ada/demo/issues/1/comments": reply(200, `[{"author_name":"bob","body":"Same here","created_at":"2026-10-02T00:00:00Z"}]`),
	})
	if err := h.run("issue", "view", "#1", "-R", "ada/demo"); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Title    string           `json:"title"`
		Comments []map[string]any `json:"comments"`
	}
	if err := json.Unmarshal(h.out.Bytes(), &got); err != nil || got.Title != "Crash on start" || len(got.Comments) != 1 {
		t.Fatalf("output = %s (%v)", h.out.String(), err)
	}

	h.app.stdoutTTY = true
	if err := h.run("issue", "view", "1", "-R", "ada/demo"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"#1 Crash on start", "It dies.", "bob commented", "Same here", "/ada/demo/issues/1"} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("view lacks %q:\n%s", want, h.out.String())
		}
	}
	if err := h.run("issue", "view", "abc", "-R", "ada/demo"); err == nil {
		t.Error("a non-numeric issue should fail")
	}
}

func TestIssueCreateAppliesLabelsAndAssignees(t *testing.T) {
	h := newRepoHarness(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /api/repos/ada/demo/labels/":             reply(200, `[{"id":7,"name":"Bug"},{"id":8,"name":"ui"}]`),
		"POST /api/repos/ada/demo/issues/":            reply(201, openIssue),
		"POST /api/repos/ada/demo/issues/1/labels/7":  reply(204, ``),
		"POST /api/repos/ada/demo/issues/1/assignees": reply(204, ``),
	})
	if err := h.run("issue", "create", "-R", "ada/demo", "--title", "Crash on start", "--body", "It dies.", "--label", "bug", "--assignee", "bob"); err != nil {
		t.Fatal(err)
	}
	want := []string{"GET /api/repos/ada/demo/labels/", "POST /api/repos/ada/demo/issues/", "POST /api/repos/ada/demo/issues/1/labels/7", "POST /api/repos/ada/demo/issues/1/assignees"}
	if strings.Join(h.reqs, "|") != strings.Join(want, "|") {
		t.Errorf("requests = %v", h.reqs)
	}
	if h.bodies[1] != `{"body":"It dies.","title":"Crash on start"}` || h.bodies[3] != `{"username":"bob"}` {
		t.Errorf("bodies = %q", h.bodies)
	}
	if !strings.Contains(h.out.String(), `"number":1`) {
		t.Errorf("output = %s", h.out.String())
	}

	h.app.stdoutTTY = true
	if err := h.run("issue", "create", "-R", "ada/demo", "--title", "x"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "Created #1") {
		t.Errorf("output = %s", h.out.String())
	}
}

func TestIssueCreateUnknownLabelCreatesNothing(t *testing.T) {
	h := newRepoHarness(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /api/repos/ada/demo/labels/": reply(200, `[{"id":7,"name":"bug"}]`),
	})
	err := h.run("issue", "create", "-R", "ada/demo", "--title", "t", "--label", "nope")
	if err == nil || !strings.Contains(err.Error(), `"nope"`) || !strings.Contains(err.Error(), "no issue was created") {
		t.Errorf("err = %v", err)
	}
	for _, r := range h.reqs {
		if strings.HasPrefix(r, "POST") {
			t.Errorf("unexpected %s", r)
		}
	}
}

func TestIssueCreateAssigneeFailureReportsCreatedIssue(t *testing.T) {
	h := newRepoHarness(t, map[string]func(http.ResponseWriter, *http.Request){
		"POST /api/repos/ada/demo/issues/":            reply(201, openIssue),
		"POST /api/repos/ada/demo/issues/1/assignees": reply(500, `{"error":"internal server error"}`),
	})
	err := h.run("issue", "create", "-R", "ada/demo", "--title", "t", "--assignee", "ghost")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"#1 was created", `"ghost"`, "internal server error", "/ada/demo/issues/1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
}

func TestIssueCreateRequiresTitle(t *testing.T) {
	h := newRepoHarness(t, nil)
	if err := h.run("issue", "create", "-R", "ada/demo", "--body", "x"); err == nil {
		t.Error("a missing title should fail")
	}
}

func TestIssueCreateBodyFromFileAndEditor(t *testing.T) {
	h := newRepoHarness(t, map[string]func(http.ResponseWriter, *http.Request){
		"POST /api/repos/ada/demo/issues/": reply(201, openIssue),
	})
	file := filepath.Join(t.TempDir(), "b.md")
	if err := os.WriteFile(file, []byte("from file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.run("issue", "create", "-R", "ada/demo", "--title", "t", "--body-file", file); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.bodies[0], `"body":"from file"`) {
		t.Errorf("body = %s", h.bodies[0])
	}
	if err := h.run("issue", "create", "-R", "ada/demo", "--title", "t", "--body", "x", "--body-file", file); err == nil {
		t.Error("--body with --body-file should fail")
	}

	h.stdin = "from stdin\n"
	if err := h.run("issue", "create", "-R", "ada/demo", "--title", "t", "--body-file", "-"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.bodies[len(h.bodies)-1], `"body":"from stdin\n"`) {
		t.Errorf("body = %s", h.bodies[len(h.bodies)-1])
	}

	h.app.stdinTTY, h.app.stdoutTTY = true, true
	h.env["EDITOR"] = "fake-editor --wait"
	var gotEditor string
	orig := runEditor
	runEditor = func(ctx context.Context, a *app, editor, path string) error {
		gotEditor = editor
		return os.WriteFile(path, []byte("typed in editor\n"), 0o600)
	}
	t.Cleanup(func() { runEditor = orig })
	if err := h.run("issue", "create", "-R", "ada/demo", "--title", "t"); err != nil {
		t.Fatal(err)
	}
	if gotEditor != "fake-editor --wait" || !strings.Contains(h.bodies[len(h.bodies)-1], `"body":"typed in editor"`) {
		t.Errorf("editor = %q, body = %s", gotEditor, h.bodies[len(h.bodies)-1])
	}

	delete(h.env, "EDITOR")
	if err := h.run("issue", "create", "-R", "ada/demo", "--title", "t"); err == nil || !strings.Contains(err.Error(), "EDITOR") {
		t.Errorf("err = %v", err)
	}
}

func TestIssueCommentPostsBody(t *testing.T) {
	h := newRepoHarness(t, map[string]func(http.ResponseWriter, *http.Request){
		"POST /api/repos/ada/demo/issues/3/comments": reply(201, `{"author_name":"ada","body":"hi"}`),
	})
	if err := h.run("issue", "comment", "3", "-R", "ada/demo", "--body", "hi"); err != nil {
		t.Fatal(err)
	}
	if h.bodies[0] != `{"body":"hi"}` {
		t.Errorf("body = %s", h.bodies[0])
	}
	if err := h.run("issue", "comment", "3", "-R", "ada/demo"); err == nil {
		t.Error("an empty comment should fail before any request")
	}
	if len(h.reqs) != 1 {
		t.Errorf("requests = %v", h.reqs)
	}
}

func TestIssueCloseSendsClosedState(t *testing.T) {
	h := newRepoHarness(t, map[string]func(http.ResponseWriter, *http.Request){
		"PATCH /api/repos/ada/demo/issues/2": reply(200, closedIssue),
	})
	h.app.stdoutTTY = true
	if err := h.run("issue", "close", "2", "-R", "ada/demo"); err != nil {
		t.Fatal(err)
	}
	if h.bodies[0] != `{"state":"closed"}` || !strings.Contains(h.out.String(), "Closed #2") {
		t.Errorf("body = %s, output = %s", h.bodies[0], h.out.String())
	}
}

func TestIssueWriteWithoutScopeGetsScopeMessage(t *testing.T) {
	h := newRepoHarness(t, map[string]func(http.ResponseWriter, *http.Request){
		"PATCH /api/repos/ada/demo/issues/2": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope", scope="issues:write"`)
			w.WriteHeader(http.StatusForbidden)
		},
	})
	err := h.run("issue", "close", "2", "-R", "ada/demo")
	if err == nil || !strings.Contains(err.Error(), `lacks scope "issues:write"`) {
		t.Errorf("err = %v", err)
	}
}
