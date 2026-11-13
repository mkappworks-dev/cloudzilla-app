package handler_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func (e *actionsEnv) api() string { return "/api/repos" + e.r.path + "/actions" }

type apiRunBody struct {
	Number      int    `json:"number"`
	Status      string `json:"status"`
	Attempt     int    `json:"attempt"`
	WorkflowRef string `json:"workflow_path"`
	HTMLURL     string `json:"html_url"`
	SHA         string `json:"sha"`
	Jobs        []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		ID     int64  `json:"id"`
	} `json:"jobs"`
}

func TestActionsAPI_ListsAndReadsRuns(t *testing.T) {
	e := newActionsEnv(t)

	list := e.call(t, http.MethodGet, e.api()+"/runs", e.reader.token, nil)
	wantCode(t, list, http.StatusOK)
	body := decodeBody[struct {
		Runs    []apiRunBody `json:"runs"`
		HasMore bool         `json:"has_more"`
	}](t, list)
	if len(body.Runs) != 1 || body.Runs[0].Number != 1 || body.Runs[0].Status != "queued" || body.Runs[0].SHA != e.sha ||
		body.Runs[0].HTMLURL != "http://localhost:8080"+e.base()+"/runs/1" {
		t.Errorf("list = %+v", body)
	}
	none := decodeBody[struct{ Runs []apiRunBody }](t, e.call(t, http.MethodGet, e.api()+"/runs?status=success", e.reader.token, nil))
	if len(none.Runs) != 0 {
		t.Errorf("status filter ignored: %+v", none.Runs)
	}
	wantCode(t, e.call(t, http.MethodGet, e.api()+"/runs?status=bogus", e.reader.token, nil), http.StatusBadRequest)

	one := decodeBody[apiRunBody](t, e.call(t, http.MethodGet, e.api()+"/runs/1", e.reader.token, nil))
	if one.Number != 1 || len(one.Jobs) != 2 || one.Jobs[0].Name != "test" {
		t.Errorf("run = %+v", one)
	}
	wantCode(t, e.call(t, http.MethodGet, e.api()+"/runs/99", e.reader.token, nil), http.StatusNotFound)
}

func TestActionsAPI_CancelAndRerunNeedWriteAndWork(t *testing.T) {
	e := newActionsEnv(t)
	wantCode(t, e.call(t, http.MethodPost, e.api()+"/runs/1/cancel", e.reader.token, nil), http.StatusForbidden)
	wantCode(t, e.call(t, http.MethodPost, e.api()+"/runs/1/rerun", e.reader.token, nil), http.StatusForbidden)
	wantCode(t, e.call(t, http.MethodPost, e.api()+"/runs/1/rerun", e.writer.token, nil), http.StatusConflict)

	cancelled := e.call(t, http.MethodPost, e.api()+"/runs/1/cancel", e.writer.token, nil)
	wantCode(t, cancelled, http.StatusOK)
	if got := decodeBody[apiRunBody](t, cancelled); got.Status != "cancelled" {
		t.Errorf("after cancel: %+v", got)
	}
	wantCode(t, e.call(t, http.MethodPost, e.api()+"/runs/1/cancel", e.writer.token, nil), http.StatusConflict)

	rerun := e.call(t, http.MethodPost, e.api()+"/runs/1/rerun", e.writer.token, map[string]bool{"failed_only": false})
	wantCode(t, rerun, http.StatusOK)
	if got := decodeBody[apiRunBody](t, rerun); got.Attempt != 2 || got.Status != "queued" {
		t.Errorf("after rerun: %+v", got)
	}
	wantCode(t, e.call(t, http.MethodPost, e.api()+"/runs/99/cancel", e.writer.token, nil), http.StatusNotFound)
}

func TestActionsAPI_DispatchStartsARunAndRefusesWhatCannotRun(t *testing.T) {
	e := newActionsEnv(t)
	c := testutil.NewPushClient(t, e.r.git)
	wf := fmt.Sprintf("name: Manual\non: workflow_dispatch\njobs:\n  go:\n    runs-on: %s\n    steps:\n      - run: echo hi\n", e.label)
	sha := c.Commit(plumbing.NewHash(e.sha), map[string]string{".cloudzilla/workflows/manual.yml": wf}, "add manual")
	e.push(t, c, &packp.Command{Name: mainRef, Old: plumbing.NewHash(e.sha), New: sha})

	body := map[string]string{"workflow": "manual.yml", "ref": "main"}
	wantCode(t, e.call(t, http.MethodPost, e.api()+"/dispatch", e.reader.token, body), http.StatusForbidden)
	ok := e.call(t, http.MethodPost, e.api()+"/dispatch", e.writer.token, body)
	wantCode(t, ok, http.StatusCreated)
	if got := decodeBody[apiRunBody](t, ok); got.WorkflowRef != ".cloudzilla/workflows/manual.yml" || got.Status != "queued" {
		t.Errorf("dispatched = %+v", got)
	}
	wantCode(t, e.call(t, http.MethodPost, e.api()+"/dispatch", e.writer.token, map[string]string{"workflow": "ci.yml", "ref": "main"}), http.StatusUnprocessableEntity)
	wantCode(t, e.call(t, http.MethodPost, e.api()+"/dispatch", e.writer.token, map[string]string{"workflow": "manual.yml", "ref": "nope"}), http.StatusUnprocessableEntity)
	wantCode(t, e.call(t, http.MethodPost, e.api()+"/dispatch", e.writer.token, map[string]string{"ref": "main"}), http.StatusBadRequest)
}

func TestActionsAPI_TokenScopesAreEnforced(t *testing.T) {
	e := newActionsEnv(t)
	read := seedPAT(t, e.db, e.writer.id, "repo:read")
	write := seedPAT(t, e.db, e.writer.id, "repo:write")
	issues := seedPAT(t, e.db, e.writer.id, "issues:write")

	wantCode(t, e.call(t, http.MethodGet, e.api()+"/runs", read, nil), http.StatusOK)
	wantCode(t, e.call(t, http.MethodGet, e.api()+"/runs/1", read, nil), http.StatusOK)
	for _, path := range []string{"/runs/1/cancel", "/runs/1/rerun", "/dispatch"} {
		rr := e.call(t, http.MethodPost, e.api()+path, read, nil)
		if rr.Code != http.StatusForbidden || rr.Body.String() != `{"error":"insufficient_scope"}` {
			t.Errorf("repo:read POST %s = %d %s", path, rr.Code, rr.Body.String())
		}
	}
	wantCode(t, e.call(t, http.MethodPost, e.api()+"/runs/1/cancel", issues, nil), http.StatusForbidden)
	wantCode(t, e.call(t, http.MethodPost, e.api()+"/runs/1/cancel", write, nil), http.StatusOK)
	wantCode(t, e.call(t, http.MethodGet, e.api()+fmt.Sprintf("/runs/1/jobs/%d/logs", e.firstJobID(t)), read, nil), http.StatusOK)
}

func (e *actionsEnv) firstJobID(t *testing.T) int64 {
	t.Helper()
	var id int64
	if err := e.db.QueryRow(`SELECT j.id FROM workflow_jobs j JOIN workflow_runs r ON r.id = j.run_id WHERE r.repo_id = $1 AND r.number = 1 ORDER BY j.id LIMIT 1`, e.r.id).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestActionsAPI_PrivateRepoLooksMissingToANonReader(t *testing.T) {
	e := newActionsEnv(t)
	testutil.Exec(t, e.db, `UPDATE repositories SET private = true WHERE id = $1`, e.r.id)
	stranger := seedSignedInUser(t, e.db)
	missing := "/api/repos/" + e.r.owner.name + "/nosuchrepo/actions"

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/runs"}, {http.MethodGet, "/runs/1"}, {http.MethodPost, "/runs/1/cancel"},
		{http.MethodPost, "/runs/1/rerun"}, {http.MethodPost, "/dispatch"},
	} {
		for name, tok := range map[string]string{"signed out": "", "stranger": stranger.token} {
			got := e.call(t, tc.method, e.api()+tc.path, tok, nil)
			want := e.call(t, tc.method, missing+tc.path, tok, nil)
			if got.Code != want.Code || got.Body.String() != want.Body.String() {
				t.Errorf("%s %s %s = %d %q, want a missing repo's %d %q", name, tc.method, tc.path, got.Code, got.Body.String(), want.Code, want.Body.String())
			}
		}
	}
}
