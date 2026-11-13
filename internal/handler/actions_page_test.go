package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/protocol/packp"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// actionsEnv is a runner env with storage, a writer and a reader added to the repo, and the first Run's page paths.
type actionsEnv struct {
	*runnerEnv
	writer, reader signedInUser
}

func newActionsEnv(t *testing.T) *actionsEnv {
	t.Helper()
	e := &actionsEnv{runnerEnv: newRunnerEnvWith(t, withSecretKey)}
	backend, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e.svcs.WithStorage(backend)
	e.writer, e.reader = seedSignedInUser(t, e.db), seedSignedInUser(t, e.db)
	testutil.Exec(t, e.db, `INSERT INTO permissions (user_id, repo_id, role) VALUES ($1, $2, 'writer')`, e.writer.id, e.r.id)
	return e
}

func (e *actionsEnv) base() string { return e.r.path + "/actions" }

func (e *actionsEnv) req(method, path, token string, form url.Values) *httptest.ResponseRecorder {
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	r := httptest.NewRequest(method, path, body)
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	e.h.ServeHTTP(rr, r)
	return rr
}

func (e *actionsEnv) get(t *testing.T, path, token string, want int) string {
	t.Helper()
	rr := e.req(http.MethodGet, path, token, nil)
	if rr.Code != want {
		t.Fatalf("GET %s = %d, want %d: %.300s", path, rr.Code, want, rr.Body.String())
	}
	return rr.Body.String()
}

func mustContain(t *testing.T, page string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(page, w) {
			t.Errorf("page is missing %q", w)
		}
	}
}

func mustNotContain(t *testing.T, page string, bad ...string) {
	t.Helper()
	for _, w := range bad {
		if strings.Contains(page, w) {
			t.Errorf("page should not contain %q", w)
		}
	}
}

func TestActionsList_ShowsRunsAndTheTabToAReader(t *testing.T) {
	e := newActionsEnv(t)
	page := e.get(t, e.base(), e.reader.token, http.StatusOK)
	mustContain(t, page, "add ci", "CI", `href="`+e.base()+`/runs/1"`, "Queued", `href="`+e.base()+`"`, "Commit statuses")
	// A reader sees no Run workflow popover and no Approve.
	mustNotContain(t, page, "Run a workflow by hand")
}

func TestActionsList_FiltersByStatusAndSaysWhenNothingMatches(t *testing.T) {
	e := newActionsEnv(t)
	mustContain(t, e.get(t, e.base()+"?status=queued", e.reader.token, http.StatusOK), `/runs/1"`)
	page := e.get(t, e.base()+"?status=success", e.reader.token, http.StatusOK)
	mustContain(t, page, "No runs match these filters.", "status Succeeded")
	mustNotContain(t, page, `/runs/1"`)
	// An unknown status is ignored rather than echoed.
	mustNotContain(t, e.get(t, e.base()+"?status=%3Cscript%3E", e.reader.token, http.StatusOK), "<script>alert")
}

func TestActionsList_EmptyRepoExplainsWorkflowsAndExternalStatuses(t *testing.T) {
	e := newCIPushEnv(t)
	a := &actionsEnv{runnerEnv: &runnerEnv{ciPushEnv: e}}
	page := a.get(t, e.r.path+"/actions", e.r.owner.token, http.StatusOK)
	mustContain(t, page, "No workflows in this repository yet.", ".cloudzilla/workflows/", "Commit statuses")
}

func TestActionsList_CommitStatusesFilterListsExternalCI(t *testing.T) {
	e := newActionsEnv(t)
	repo, _ := e.svcs.Repo.GetByID(context.Background(), e.r.id)
	cs := &model.CommitStatus{RepoID: e.r.id, SHA: e.sha, Context: "buildkite/linux", State: model.CommitStatusSuccess,
		Description: "Build 7 passed", CreatorID: e.r.owner.id}
	if err := e.svcs.CommitStatus.Upsert(context.Background(), repo.OwnerName, repo.Name, e.sha, cs); err != nil {
		t.Fatal(err)
	}
	page := e.get(t, e.base()+"?source=statuses", e.reader.token, http.StatusOK)
	mustContain(t, page, "buildkite/linux", "Build 7 passed")
}

func TestChecks_RedirectsToActions(t *testing.T) {
	e := newActionsEnv(t)
	rr := e.req(http.MethodGet, e.r.path+"/checks?source=statuses", e.reader.token, nil)
	if rr.Code != http.StatusMovedPermanently || rr.Header().Get("Location") != e.base()+"?source=statuses" {
		t.Fatalf("got %d %q", rr.Code, rr.Header().Get("Location"))
	}
}

func TestActionsTab_IsHiddenWhenRepoCIIsOffUnlessItHasRuns(t *testing.T) {
	e := newCIPushEnv(t)
	a := &actionsEnv{runnerEnv: &runnerEnv{ciPushEnv: e}}
	if err := e.svcs.WorkflowRun.SetRepoEnabled(context.Background(), e.r.id, false); err != nil {
		t.Fatal(err)
	}
	mustNotContain(t, a.get(t, e.r.path, e.r.owner.token, http.StatusOK), `href="`+e.r.path+`/actions"`)

	n := newActionsEnv(t)
	if err := n.svcs.WorkflowRun.SetRepoEnabled(context.Background(), n.r.id, false); err != nil {
		t.Fatal(err)
	}
	mustContain(t, n.get(t, n.r.path, n.r.owner.token, http.StatusOK), `href="`+n.r.path+`/actions"`)
}

func TestActionsRun_PageShowsGraphJobsAndQueuedNoRunnerState(t *testing.T) {
	e := newActionsEnv(t)
	page := e.get(t, e.base()+"/runs/1", e.reader.token, http.StatusOK)
	// The pushed workflow's label has no runner, so the Job says which label is missing.
	mustContain(t, page, "Job graph", ">test<", ">build<", "No runner with label "+e.label, "Reported as", "Attempt")
	mustNotContain(t, page, "Cancel run", "Re-run")

	writer := e.get(t, e.base()+"/runs/1", e.writer.token, http.StatusOK)
	mustContain(t, writer, "Cancel run", e.base()+"/runs/1/cancel")
	e.get(t, e.base()+"/runs/99", e.reader.token, http.StatusNotFound)
}

func TestActionsRun_RunningJobTailsByOffsetAndStopsWhenItEnds(t *testing.T) {
	e := newActionsEnv(t)
	spec := e.claim(t, e.registerRunner(t, nil))
	le := &logAPIEnv{runnerEnv: e.runnerEnv, spec: spec, run: spec.RunNumber}
	wantCode(t, le.postLog(t, spec.Token, 0, 0, "alpha\n\x1b[32mgreen\x1b[0m\n"), http.StatusOK)

	page := e.get(t, e.base()+"/runs/1?job="+fmt.Sprint(spec.JobID), e.reader.token, http.StatusOK)
	mustContain(t, page, `id="L1"`, `id="L2"`, "alpha", `class="text-success"`, "Live", `hx-trigger="every 2s"`,
		fmt.Sprintf("jobs/%d/tail?offset=2", spec.JobID))

	wantCode(t, le.postLog(t, spec.Token, 1, 0, "beta\n"), http.StatusOK)
	tail := e.get(t, fmt.Sprintf("%s/runs/1/jobs/%d/tail?offset=2", e.base(), spec.JobID), e.reader.token, http.StatusOK)
	mustContain(t, tail, `hx-swap-oob="beforeend:#step-0-log"`, `id="L3"`, "beta", "tail?offset=3", `hx-trigger="every 2s"`)
	mustNotContain(t, tail, "alpha")

	if err := e.svcs.WorkflowRun.FinishJob(context.Background(), spec.JobID, "success", ""); err != nil {
		t.Fatal(err)
	}
	done := e.req(http.MethodGet, fmt.Sprintf("%s/runs/1/jobs/%d/tail?offset=3", e.base(), spec.JobID), e.reader.token, nil)
	if done.Header().Get("HX-Refresh") != "true" || strings.Contains(done.Body.String(), "hx-trigger") {
		t.Errorf("a finished job's tail should refresh the page and stop polling: %v %q", done.Header(), done.Body.String())
	}
}

func TestActionsRun_ControlsNeedWriteAndHideBehindTheSameNotFound(t *testing.T) {
	e := newActionsEnv(t)
	cancel := e.base() + "/runs/1/cancel"
	if rr := e.req(http.MethodPost, cancel, "", nil); rr.Code != http.StatusUnauthorized && rr.Code != http.StatusForbidden {
		t.Errorf("signed-out cancel = %d", rr.Code)
	}
	if rr := e.req(http.MethodPost, cancel, e.reader.token, nil); rr.Code != http.StatusForbidden {
		t.Errorf("reader cancel = %d", rr.Code)
	}
	for _, path := range []string{"/runs/1/rerun", "/runs/1/approve", "/dispatch"} {
		if rr := e.req(http.MethodPost, e.base()+path, e.reader.token, url.Values{"workflow": {"ci.yml"}, "ref": {"main"}}); rr.Code != http.StatusForbidden {
			t.Errorf("reader POST %s = %d", path, rr.Code)
		}
	}

	rr := e.req(http.MethodPost, cancel, e.writer.token, nil)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != e.base()+"/runs/1" {
		t.Fatalf("writer cancel = %d %q", rr.Code, rr.Header().Get("Location"))
	}
	page := e.get(t, e.base()+"/runs/1", e.reader.token, http.StatusOK)
	mustContain(t, page, "Cancelled by "+e.writer.name)
	// A second cancel is refused with words, not an error page.
	rr = e.req(http.MethodPost, cancel, e.writer.token, nil)
	if loc := rr.Header().Get("Location"); loc != e.base()+"/runs/1?err=finished" {
		t.Errorf("second cancel went to %q", loc)
	}
	mustContain(t, e.get(t, e.base()+"/runs/1?err=finished", e.writer.token, http.StatusOK), "That run has already finished.")
	mustNotContain(t, e.get(t, e.base()+"/runs/1?err=%3Cb%3Ex", e.writer.token, http.StatusOK), "<b>x")
}

func TestActionsRun_RerunFailedRunsOnlyTheFailedJob(t *testing.T) {
	e := newActionsEnv(t)
	tok := e.registerRunner(t, nil)
	spec := e.claim(t, tok)
	wantCode(t, e.call(t, http.MethodPost, fmt.Sprintf("/api/runner/jobs/%d/finish", spec.JobID), tok, map[string]string{"result": "failure", "message": "boom"}), http.StatusNoContent)

	page := e.get(t, e.base()+"/runs/1", e.writer.token, http.StatusOK)
	mustContain(t, page, "Re-run failed jobs", "Re-run all jobs", `name="failed" value="1"`)
	if rr := e.req(http.MethodPost, e.base()+"/runs/1/rerun", e.writer.token, url.Values{"failed": {"1"}}); rr.Code != http.StatusSeeOther || strings.Contains(rr.Header().Get("Location"), "err=") {
		t.Fatalf("rerun = %d %q", rr.Code, rr.Header().Get("Location"))
	}
	run, _ := e.svcs.WorkflowRun.GetRun(context.Background(), e.r.id, 1)
	if run.Attempt != 2 {
		t.Errorf("attempt = %d", run.Attempt)
	}
}

func TestActionsDispatch_WriterPicksAWorkflowAndRef(t *testing.T) {
	e := newCIPushEnv(t)
	a := &actionsEnv{runnerEnv: &runnerEnv{ciPushEnv: e}}
	writer := seedSignedInUser(t, e.db)
	testutil.Exec(t, e.db, `INSERT INTO permissions (user_id, repo_id, role) VALUES ($1, $2, 'writer')`, writer.id, e.r.id)
	wf := "name: Manual\non: workflow_dispatch\njobs:\n  go:\n    runs-on: x\n    steps:\n      - run: echo hi\n"
	c := testutil.NewPushClient(t, e.r.git)
	sha := c.Commit(e.r.mainTip, map[string]string{".cloudzilla/workflows/manual.yml": wf}, "add manual")
	e.push(t, c, &packp.Command{Name: mainRef, Old: e.r.mainTip, New: sha})

	page := a.get(t, e.r.path+"/actions", writer.token, http.StatusOK)
	mustContain(t, page, "Run a workflow by hand", `value="manual.yml"`, `value="main"`, "workflow_dispatch")
	mustNotContain(t, a.get(t, e.r.path+"/actions", seedSignedInUser(t, e.db).token, http.StatusOK), "Run a workflow by hand")

	rr := a.req(http.MethodPost, e.r.path+"/actions/dispatch", writer.token, url.Values{"workflow": {"manual.yml"}, "ref": {"main"}})
	if rr.Code != http.StatusSeeOther || !strings.HasPrefix(rr.Header().Get("Location"), e.r.path+"/actions/runs/") || strings.Contains(rr.Header().Get("Location"), "err=") {
		t.Fatalf("dispatch = %d %q", rr.Code, rr.Header().Get("Location"))
	}
	if rr := a.req(http.MethodPost, e.r.path+"/actions/dispatch", writer.token, url.Values{"workflow": {"../x.yml"}, "ref": {"main"}}); !strings.Contains(rr.Header().Get("Location"), "err=") {
		t.Errorf("a path-like workflow name was accepted: %q", rr.Header().Get("Location"))
	}
}

func TestActionsRun_ForkRunWaitsForApprovalAndOnlyAWriterSeesTheButton(t *testing.T) {
	e := newActionsEnv(t)
	testutil.Exec(t, e.db, `UPDATE workflow_runs SET status = 'waiting', no_secrets = true WHERE repo_id = $1`, e.r.id)
	testutil.Exec(t, e.db, `UPDATE workflow_jobs SET status = 'waiting' WHERE run_id IN (SELECT id FROM workflow_runs WHERE repo_id = $1)`, e.r.id)

	reader := e.get(t, e.base()+"/runs/1", e.reader.token, http.StatusOK)
	mustContain(t, reader, "Approval required", "A writer has to approve it")
	mustNotContain(t, reader, "Approve and run")
	writer := e.get(t, e.base()+"/runs/1", e.writer.token, http.StatusOK)
	mustContain(t, writer, "Approve and run", e.base()+"/runs/1/approve", "It gets no secrets")
	mustContain(t, e.get(t, e.base(), e.writer.token, http.StatusOK), ">Approve</button>")
	mustNotContain(t, e.get(t, e.base(), e.reader.token, http.StatusOK), ">Approve</button>")

	if rr := e.req(http.MethodPost, e.base()+"/runs/1/approve", e.writer.token, nil); strings.Contains(rr.Header().Get("Location"), "err=") {
		t.Fatalf("approve: %q", rr.Header().Get("Location"))
	}
	if run, _ := e.svcs.WorkflowRun.GetRun(context.Background(), e.r.id, 1); run.Status != model.CIStatusQueued || !run.NoSecrets {
		t.Errorf("run after approve = %+v", run)
	}
}

func TestActionsRun_YAMLErrorShowsTheFileWithTheMessageUnderItsLine(t *testing.T) {
	e := newCIPushEnv(t)
	a := &actionsEnv{runnerEnv: &runnerEnv{ciPushEnv: e}}
	bad := "name: Deploy\non: push\njobs:\n  d:\n    runs-on: x\n    steps:\n      - uses: actions/checkout@v4\n"
	c := testutil.NewPushClient(t, e.r.git)
	sha := c.Commit(e.r.mainTip, map[string]string{".cloudzilla/workflows/deploy.yml": bad}, "bad")
	e.push(t, c, &packp.Command{Name: mainRef, Old: e.r.mainTip, New: sha})

	page := a.get(t, e.r.path+"/actions/runs/1", e.r.owner.token, http.StatusOK)
	mustContain(t, page, "Invalid workflow file", "No jobs ran.", "uses: actions/checkout@v4", "Invalid workflow")
	list := a.get(t, e.r.path+"/actions", e.r.owner.token, http.StatusOK)
	mustContain(t, list, "deploy.yml couldn't be parsed on main", "Invalid workflow")
}

func TestActionsRun_PrivateRepoLooksMissingToAStranger(t *testing.T) {
	e := newActionsEnv(t)
	testutil.Exec(t, e.db, `UPDATE repositories SET private = true WHERE id = $1`, e.r.id)
	stranger := seedSignedInUser(t, e.db)
	for _, p := range []string{e.base(), e.base() + "/runs/1", e.base() + "/runs/1/jobs/1/tail"} {
		e.get(t, p, stranger.token, http.StatusNotFound)
		e.get(t, p, "", http.StatusNotFound)
	}
	missing := e.get(t, "/nobody/nothing/actions", stranger.token, http.StatusNotFound)
	private := e.get(t, e.base(), stranger.token, http.StatusNotFound)
	if strings.Contains(private, e.r.name) || len(private) != len(missing) && !strings.Contains(private, "Not found") {
		t.Errorf("a private repo's 404 differs from a missing repo's")
	}
	if rr := e.req(http.MethodPost, e.base()+"/runs/1/cancel", stranger.token, nil); rr.Code != http.StatusNotFound {
		t.Errorf("stranger cancel = %d", rr.Code)
	}
}
