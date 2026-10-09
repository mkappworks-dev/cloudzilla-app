package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const pullJSON = `{"number":7,"title":"Add widgets","body":"Adds widgets.","state":"open","head_branch":"feature","base_branch":"main","author_name":"bob","is_draft":false}`

type exitErr int

func (e exitErr) Error() string { return "exit status " + string(rune('0'+int(e))) }
func (e exitErr) ExitCode() int { return int(e) }

type routes = map[string]func(http.ResponseWriter, *http.Request)

func TestPRListFiltersByStateAndRendersTable(t *testing.T) {
	h := newRepoHarness(t, routes{
		"GET /api/repos/ada/demo/pulls/": reply(200, `[`+pullJSON+`,{"number":6,"title":"Old","state":"merged","head_branch":"x","base_branch":"main","author_name":"ada"},{"number":5,"title":"Wip","state":"open","is_draft":true,"head_branch":"w","base_branch":"main","author_name":"ada"}]`),
	})
	h.app.stdoutTTY = true
	if err := h.run("pr", "list", "-R", "ada/demo"); err != nil {
		t.Fatal(err)
	}
	out := h.out.String()
	for _, want := range []string{"NUMBER", "#7", "Add widgets", "feature → main", "bob", "#5", "draft"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "Old") {
		t.Errorf("merged PR listed by default:\n%s", out)
	}

	h.app.stdoutTTY = false
	if err := h.run("pr", "list", "-R", "ada/demo", "--state", "all"); err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal(h.out.Bytes(), &got); err != nil || len(got) != 3 {
		t.Fatalf("json = %s (%v)", h.out.String(), err)
	}
	if err := h.run("pr", "list", "-R", "ada/demo", "--state", "bogus"); err == nil {
		t.Error("bad --state accepted")
	}
}

func TestPRListNullFromServerIsEmpty(t *testing.T) {
	h := newRepoHarness(t, routes{"GET /api/repos/ada/demo/pulls/": reply(200, `null`)})
	h.app.stdoutTTY = true
	if err := h.run("pr", "list", "-R", "ada/demo"); err != nil || !strings.Contains(h.out.String(), "No pull requests") {
		t.Fatalf("%v %q", err, h.out.String())
	}
}

func TestPRViewShowsReviewsAndLineComments(t *testing.T) {
	h := newRepoHarness(t, routes{
		"GET /api/repos/ada/demo/pulls/7":               reply(200, pullJSON),
		"GET /api/repos/ada/demo/pulls/7/reviews":       reply(200, `[{"author_name":"ada","state":"changes_requested","body":"Needs tests"}]`),
		"GET /api/repos/ada/demo/pulls/7/line_comments": reply(200, `[{"author_name":"ada","path":"a.go","line":12,"body":"nit"}]`),
	})
	h.app.stdoutTTY = true
	if err := h.run("pr", "view", "#7", "-R", "ada/demo"); err != nil {
		t.Fatal(err)
	}
	out := h.out.String()
	for _, want := range []string{"#7 Add widgets", "bob wants to merge feature into main", "Adds widgets.", "ada: changes requested", "Needs tests", "a.go:12", "nit", h.api.URL + "/ada/demo/pulls/7"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}

	h.app.stdoutTTY = false
	if err := h.run("pr", "view", "7", "-R", "ada/demo"); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Pull         map[string]any   `json:"pull"`
		Reviews      []map[string]any `json:"reviews"`
		LineComments []map[string]any `json:"line_comments"`
	}
	if err := json.Unmarshal(h.out.Bytes(), &got); err != nil || got.Pull["number"] != float64(7) || len(got.Reviews) != 1 || len(got.LineComments) != 1 {
		t.Fatalf("json = %s (%v)", h.out.String(), err)
	}
	if err := h.run("pr", "view", "x", "-R", "ada/demo"); err == nil {
		t.Error("non-numeric argument accepted")
	}
}

func createRoutes(post func(http.ResponseWriter, *http.Request)) routes {
	return routes{
		"GET /api/repos/ada/demo":         reply(200, repoJSON),
		"POST /api/repos/ada/demo/pulls/": post,
	}
}

func created(ctype *string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if ctype != nil {
			*ctype = r.Header.Get("Content-Type")
		}
		w.WriteHeader(201)
		_, _ = w.Write([]byte(pullJSON))
	}
}

// sent decodes the body of the last request that matched "METHOD /path"; the harness has already drained r.Body.
func sent(t *testing.T, h *repoHarness, req string) map[string]any {
	t.Helper()
	for i := len(h.reqs) - 1; i >= 0; i-- {
		if h.reqs[i] == req {
			var m map[string]any
			if err := json.Unmarshal([]byte(h.bodies[i]), &m); err != nil {
				t.Fatalf("%s body %q: %v", req, h.bodies[i], err)
			}
			return m
		}
	}
	return nil
}

func TestPRCreateSendsServerFieldNamesWithDefaultBase(t *testing.T) {
	var ctype string
	h := newRepoHarness(t, createRoutes(created(&ctype)))
	h.app.stdoutTTY = true
	if err := h.run("pr", "create", "-R", "ada/demo", "--head", "feature", "--title", "Add widgets", "--body", "Adds widgets."); err != nil {
		t.Fatal(err)
	}
	body := sent(t, h, "POST /api/repos/ada/demo/pulls/")
	if ctype != "application/json" || body["head_branch"] != "feature" || body["base_branch"] != "main" || body["title"] != "Add widgets" || body["body"] != "Adds widgets." {
		t.Errorf("ctype=%q body=%v", ctype, body)
	}
	if !strings.Contains(h.out.String(), "Created #7") || !strings.Contains(h.out.String(), "/ada/demo/pulls/7") {
		t.Errorf("out = %q", h.out.String())
	}
	joined := strings.Join(h.gitArgs, " ")
	if !strings.Contains(joined, "ls-remote --exit-code --heads "+h.api.URL+"/ada/demo.git feature") {
		t.Errorf("git args = %v", h.gitArgs)
	}
	for _, a := range h.gitArgs {
		if strings.Contains(a, "czp_good") {
			t.Errorf("token in argv: %v", h.gitArgs)
		}
	}
	if !strings.Contains(strings.Join(h.gitEnv, " "), cloneTokenEnv+"=czp_good") {
		t.Errorf("git env = %v", h.gitEnv)
	}
}

func TestPRCreateDefaultsHeadToCurrentBranch(t *testing.T) {
	h := newRepoHarness(t, createRoutes(created(nil)))
	dir := remoteCheckout(t, h.api.URL+"/ada/demo.git")
	if out, err := exec.Command("git", "-C", dir, "checkout", "-q", "-b", "topic").CombinedOutput(); err != nil {
		t.Skipf("git: %v %s", err, out)
	}
	h.app.getwd = func() (string, error) { return dir, nil }
	if err := h.run("pr", "create", "--title", "T", "--body", "b"); err != nil {
		t.Fatal(err)
	}
	if body := sent(t, h, "POST /api/repos/ada/demo/pulls/"); body["head_branch"] != "topic" {
		t.Errorf("body = %v", body)
	}
}

func TestPRCreateFailsWhenHeadNotPushed(t *testing.T) {
	posted := false
	h := newRepoHarness(t, createRoutes(func(w http.ResponseWriter, r *http.Request) { posted = true }))
	h.app.runGit = func(ctx context.Context, env []string, args ...string) error { return exitErr(2) }
	err := h.run("pr", "create", "-R", "ada/demo", "--head", "ghost", "--title", "T", "--body", "b")
	if err == nil || !strings.Contains(err.Error(), `branch "ghost" is not on the remote`) || !strings.Contains(err.Error(), "git push") {
		t.Fatalf("err = %v", err)
	}
	if posted {
		t.Error("PR created for an unpushed branch")
	}
}

func TestPRCreateReportsFailedRemoteCheck(t *testing.T) {
	h := newRepoHarness(t, createRoutes(func(w http.ResponseWriter, r *http.Request) { t.Error("posted") }))
	h.app.runGit = func(ctx context.Context, env []string, args ...string) error { return errors.New("exit status 128") }
	err := h.run("pr", "create", "-R", "ada/demo", "--head", "feature", "--title", "T", "--body", "b")
	if err == nil || !strings.Contains(err.Error(), "could not check") {
		t.Fatalf("err = %v", err)
	}
}

func TestPRCreateRequiresTitleAndDistinctBranches(t *testing.T) {
	h := newRepoHarness(t, createRoutes(func(w http.ResponseWriter, r *http.Request) { t.Error("posted") }))
	if err := h.run("pr", "create", "-R", "ada/demo", "--head", "feature", "--body", "b"); err == nil || !strings.Contains(err.Error(), "--title") {
		t.Errorf("err = %v", err)
	}
	if err := h.run("pr", "create", "-R", "ada/demo", "--head", "main", "--title", "T", "--body", "b"); err == nil || !strings.Contains(err.Error(), "both") {
		t.Errorf("err = %v", err)
	}
}

func TestPRCreateReadsBodyFile(t *testing.T) {
	h := newRepoHarness(t, createRoutes(created(nil)))
	f := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(f, []byte("from file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.run("pr", "create", "-R", "ada/demo", "--head", "feature", "--title", "T", "--body-file", f); err != nil {
		t.Fatal(err)
	}
	if body := sent(t, h, "POST /api/repos/ada/demo/pulls/"); body["body"] != "from file" {
		t.Errorf("body = %v", body)
	}
}

func mergeHarness(t *testing.T, status int, resp string) *repoHarness {
	t.Helper()
	return newRepoHarness(t, routes{
		"PATCH /api/repos/ada/demo/pulls/7": func(w http.ResponseWriter, r *http.Request) {
			if status == 403 {
				w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope", scope="repo:write"`)
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(resp))
		},
	})
}

func TestPRMergeStrategies(t *testing.T) {
	for _, tc := range []struct{ flag, want string }{{"", "ff"}, {"--ff", "ff"}, {"--merge", "merge"}, {"--squash", "squash"}} {
		h := mergeHarness(t, 200, strings.Replace(pullJSON, `"open"`, `"merged"`, 1))
		args := []string{"pr", "merge", "7", "-R", "ada/demo"}
		if tc.flag != "" {
			args = append(args, tc.flag)
		}
		if err := h.run(args...); err != nil {
			t.Fatal(err)
		}
		if body := sent(t, h, "PATCH /api/repos/ada/demo/pulls/7"); body["state"] != "merged" || body["merge_strategy"] != tc.want {
			t.Errorf("%q: body = %v", tc.flag, body)
		}
	}
	h := mergeHarness(t, 200, pullJSON)
	if err := h.run("pr", "merge", "7", "-R", "ada/demo", "--merge", "--squash"); err == nil {
		t.Error("two strategies accepted")
	}
}

func TestPRMergeReportsServerReason(t *testing.T) {
	for _, tc := range []struct {
		status int
		resp   string
		want   string
	}{
		{422, `{"error":"merge blocked: required status check \"ci\" has not passed"}`, `required status check "ci"`},
		{422, `{"error":"merge conflict in a.go"}`, "merge conflict in a.go"},
		{422, `{"error":"merge blocked: changes requested by reviewer"}`, "changes requested by reviewer"},
		{409, `{"error":"branch was updated while saving; reload and try again"}`, "branch was updated"},
	} {
		h := mergeHarness(t, tc.status, tc.resp)
		err := h.run("pr", "merge", "7", "-R", "ada/demo")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%d: err = %v", tc.status, err)
		}
	}
}

func TestPRMergeWithPullsWriteTokenSaysRepoWrite(t *testing.T) {
	h := mergeHarness(t, 403, `{"error":"insufficient_scope"}`)
	err := h.run("pr", "merge", "7", "-R", "ada/demo")
	if err == nil || !strings.Contains(err.Error(), "merging needs") || !strings.Contains(err.Error(), "repo:write") || !strings.Contains(err.Error(), "pulls:write is not enough") {
		t.Fatalf("err = %v", err)
	}
}

func TestPRCloseOnlyClosesOpenPulls(t *testing.T) {
	state := "open"
	h := newRepoHarness(t, routes{
		"GET /api/repos/ada/demo/pulls/7": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(strings.Replace(pullJSON, `"open"`, `"`+state+`"`, 1)))
		},
		"PATCH /api/repos/ada/demo/pulls/7": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(strings.Replace(pullJSON, `"open"`, `"closed"`, 1)))
		},
	})
	h.app.stdoutTTY = true
	if err := h.run("pr", "close", "7", "-R", "ada/demo"); err != nil {
		t.Fatal(err)
	}
	if body := sent(t, h, "PATCH /api/repos/ada/demo/pulls/7"); body["state"] != "closed" || !strings.Contains(h.out.String(), "Closed #7") {
		t.Errorf("body=%v out=%q", body, h.out.String())
	}

	h.reqs, h.bodies, state = nil, nil, "merged"
	err := h.run("pr", "close", "7", "-R", "ada/demo")
	if err == nil || !strings.Contains(err.Error(), "already merged") || sent(t, h, "PATCH /api/repos/ada/demo/pulls/7") != nil {
		t.Errorf("err=%v reqs=%v", err, h.reqs)
	}
}

func TestPRReviewStates(t *testing.T) {
	for _, tc := range []struct{ flag, want string }{
		{"--approve", "approved"}, {"--request-changes", "changes_requested"}, {"--comment", "commented"},
	} {
		h := newRepoHarness(t, routes{
			"POST /api/repos/ada/demo/pulls/7/reviews": func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("content type = %q", r.Header.Get("Content-Type"))
				}
				w.WriteHeader(201)
				_, _ = w.Write([]byte(`{"state":"ok"}`))
			},
		})
		if err := h.run("pr", "review", "7", "-R", "ada/demo", tc.flag, "--body", "looks fine"); err != nil {
			t.Fatal(err)
		}
		if body := sent(t, h, "POST /api/repos/ada/demo/pulls/7/reviews"); body["state"] != tc.want || body["body"] != "looks fine" {
			t.Errorf("%s: body = %v", tc.flag, body)
		}
	}
}

func TestPRReviewValidation(t *testing.T) {
	h := newRepoHarness(t, routes{
		"POST /api/repos/ada/demo/pulls/7/reviews": func(w http.ResponseWriter, r *http.Request) { t.Error("posted") },
	})
	if err := h.run("pr", "review", "7", "-R", "ada/demo", "--body", "x"); err == nil {
		t.Error("no state flag accepted")
	}
	if err := h.run("pr", "review", "7", "-R", "ada/demo", "--approve", "--comment", "--body", "x"); err == nil {
		t.Error("two state flags accepted")
	}
	if err := h.run("pr", "review", "7", "-R", "ada/demo", "--request-changes"); err == nil || !strings.Contains(err.Error(), "needs a body") {
		t.Errorf("err = %v", err)
	}
}

func TestPRReviewOwnPullIsRefusedByServer(t *testing.T) {
	h := newRepoHarness(t, routes{
		"POST /api/repos/ada/demo/pulls/7/reviews": reply(422, `{"error":"cannot review your own pull request"}`),
	})
	err := h.run("pr", "review", "7", "-R", "ada/demo", "--approve")
	if err == nil || !strings.Contains(err.Error(), "cannot review your own pull request") {
		t.Fatalf("err = %v", err)
	}
}
