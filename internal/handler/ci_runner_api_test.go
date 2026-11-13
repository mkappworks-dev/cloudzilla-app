package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	gogitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/mkappworks-dev/cloudzilla-app/internal/ci/runner"
	"github.com/mkappworks-dev/cloudzilla-app/internal/ci/runnerapi"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// runnerEnv is a CI push env with a workflow pushed whose Jobs need a label no other test uses, so a runner here
// never claims another test's Job in the shared database.
type runnerEnv struct {
	*ciPushEnv
	label string
	sha   string
}

func newRunnerEnv(t *testing.T) *runnerEnv { return newRunnerEnvWith(t, nil) }

func newRunnerEnvWith(t *testing.T, tweak func(*config.Config)) *runnerEnv {
	t.Helper()
	e := &runnerEnv{ciPushEnv: newCIPushEnvWith(t, tweak), label: "t" + testutil.UniqueSuffix(t)}
	wf := fmt.Sprintf("name: CI\non: push\njobs:\n  test:\n    runs-on: %[1]s\n    steps:\n      - run: echo one\n      - run: echo two\n"+
		"  build:\n    runs-on: %[1]s\n    needs: test\n    steps:\n      - run: echo build\n", e.label)
	c := testutil.NewPushClient(t, e.r.git)
	sha := c.Commit(e.r.mainTip, map[string]string{".cloudzilla/workflows/ci.yml": wf}, "add ci")
	e.push(t, c, &packp.Command{Name: mainRef, Old: e.r.mainTip, New: sha})
	e.sha = sha.String()
	return e
}

func (e *runnerEnv) call(t *testing.T, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	e.h.ServeHTTP(rr, req)
	return rr
}

func decodeBody[T any](t *testing.T, rr *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(rr.Body).Decode(&v); err != nil {
		t.Fatalf("decode %q: %v", rr.Body.String(), err)
	}
	return v
}

func wantCode(t *testing.T, rr *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rr.Code != want {
		t.Fatalf("status = %d, want %d: %s", rr.Code, want, rr.Body.String())
	}
}

// registerRunner registers an instance runner (or one for repo when repo is not nil) with the env's label.
func (e *runnerEnv) registerRunner(t *testing.T, repo *model.Repository, labels ...string) string {
	t.Helper()
	if len(labels) == 0 {
		labels = []string{e.label}
	}
	reg, err := e.svcs.CIRunner.CreateRegistrationToken(context.Background(), repo, e.r.owner.id)
	if err != nil {
		t.Fatalf("registration token: %v", err)
	}
	rr := e.call(t, http.MethodPost, "/api/runner/register", reg, runnerapi.RegisterRequest{Name: "fake", Labels: labels})
	wantCode(t, rr, http.StatusCreated)
	return decodeBody[runnerapi.RegisterResponse](t, rr).Token
}

func (e *runnerEnv) claim(t *testing.T, token string) *runnerapi.JobSpec {
	t.Helper()
	rr := e.call(t, http.MethodPost, "/api/runner/claim", token, runnerapi.ClaimRequest{})
	if rr.Code == http.StatusNoContent {
		return nil
	}
	wantCode(t, rr, http.StatusOK)
	spec := decodeBody[runnerapi.JobSpec](t, rr)
	return &spec
}

func (e *runnerEnv) job(t *testing.T, id int64) *model.WorkflowJob {
	t.Helper()
	j, err := e.svcs.WorkflowRun.GetJob(context.Background(), id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	return j
}

func (e *runnerEnv) run(t *testing.T) *model.WorkflowRun {
	t.Helper()
	runs := e.runs(t)
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	return &runs[0]
}

func jobPath(id int64, rest string) string { return fmt.Sprintf("/api/runner/jobs/%d/%s", id, rest) }

func TestRunnerAPI_FakeRunnerRunsAJobEndToEnd(t *testing.T) {
	e := newRunnerEnv(t)
	token := e.registerRunner(t, nil)

	spec := e.claim(t, token)
	if spec == nil || spec.Name != "test" || len(spec.Steps) != 2 || spec.SHA != e.sha || spec.Steps[1].Run != "echo two" {
		t.Fatalf("claimed %+v", spec)
	}
	if !strings.HasPrefix(spec.Token, "czj_") || !strings.HasSuffix(spec.CloneURL, e.r.path+".git") || spec.Repository != e.r.owner.name+"/"+e.r.name {
		t.Errorf("clone details = %q %q %q", spec.Token, spec.CloneURL, spec.Repository)
	}
	if e.job(t, spec.JobID).Status != model.CIStatusRunning || e.run(t).Status != model.CIStatusRunning {
		t.Errorf("job and run should be running")
	}
	// build needs test, so nothing else is claimable yet.
	if again := e.claim(t, token); again != nil {
		t.Fatalf("claimed %q while its needs are open", again.Name)
	}

	wantCode(t, e.call(t, http.MethodPost, jobPath(spec.JobID, "heartbeat"), token, nil), http.StatusOK)
	for pos := 0; pos < 2; pos++ {
		wantCode(t, e.call(t, http.MethodPost, jobPath(spec.JobID, fmt.Sprintf("steps/%d", pos)), token, runnerapi.StepRequest{Status: "running"}), http.StatusNoContent)
		wantCode(t, e.call(t, http.MethodPost, jobPath(spec.JobID, fmt.Sprintf("steps/%d", pos)), token, runnerapi.StepRequest{Status: "success"}), http.StatusNoContent)
	}
	steps, _ := e.svcs.WorkflowRun.ListSteps(context.Background(), spec.JobID)
	if steps[0].Status != model.CIStatusSuccess || steps[1].FinishedAt == nil {
		t.Errorf("steps = %+v", steps)
	}
	wantCode(t, e.call(t, http.MethodPost, jobPath(spec.JobID, "finish"), token, runnerapi.FinishRequest{Result: "success"}), http.StatusNoContent)

	next := e.claim(t, token)
	if next == nil || next.Name != "build" || next.Needs["test"] != "success" {
		t.Fatalf("second claim = %+v", next)
	}
	wantCode(t, e.call(t, http.MethodPost, jobPath(next.JobID, "finish"), token, runnerapi.FinishRequest{Result: "failure", Message: "exit status 2"}), http.StatusNoContent)
	if run := e.run(t); run.Status != model.CIStatusFailure {
		t.Errorf("run = %s, want failure", run.Status)
	}
	statuses, _ := e.svcs.CommitStatus.List(context.Background(), e.r.owner.name, e.r.name, e.sha)
	got := map[string]model.CommitStatusState{}
	for _, cs := range statuses {
		got[cs.Context] = cs.State
	}
	if got["ci/test"] != model.CommitStatusSuccess || got["ci/build"] != model.CommitStatusFailure {
		t.Errorf("statuses = %v", got)
	}
	// A finished Job cannot be finished twice.
	wantCode(t, e.call(t, http.MethodPost, jobPath(next.JobID, "finish"), token, runnerapi.FinishRequest{Result: "success"}), http.StatusConflict)
}

func TestRunnerAPI_RegistrationTokenIsOneTimeAndTokensAreChecked(t *testing.T) {
	e := newRunnerEnv(t)
	reg, _ := e.svcs.CIRunner.CreateRegistrationToken(context.Background(), nil, e.r.owner.id)
	body := runnerapi.RegisterRequest{Name: "r", Labels: []string{e.label}}
	wantCode(t, e.call(t, http.MethodPost, "/api/runner/register", reg, body), http.StatusCreated)
	wantCode(t, e.call(t, http.MethodPost, "/api/runner/register", reg, body), http.StatusUnauthorized)
	wantCode(t, e.call(t, http.MethodPost, "/api/runner/register", "czreg_nope", body), http.StatusUnauthorized)
	wantCode(t, e.call(t, http.MethodPost, "/api/runner/register", "", body), http.StatusUnauthorized)

	// A registration token is not a runner token.
	reg2, _ := e.svcs.CIRunner.CreateRegistrationToken(context.Background(), nil, e.r.owner.id)
	wantCode(t, e.call(t, http.MethodPost, "/api/runner/claim", reg2, nil), http.StatusUnauthorized)
	wantCode(t, e.call(t, http.MethodPost, "/api/runner/claim", "", nil), http.StatusUnauthorized)
	// A user's session or token is not one either.
	wantCode(t, e.call(t, http.MethodPost, "/api/runner/claim", e.r.owner.token, nil), http.StatusUnauthorized)

	reg3, _ := e.svcs.CIRunner.CreateRegistrationToken(context.Background(), nil, e.r.owner.id)
	wantCode(t, e.call(t, http.MethodPost, "/api/runner/register", reg3, runnerapi.RegisterRequest{Name: "r", Labels: nil}), http.StatusBadRequest)
}

func TestRunnerAPI_RevokedRunnerIsRefusedAtOnce(t *testing.T) {
	e := newRunnerEnv(t)
	token := e.registerRunner(t, nil)
	spec := e.claim(t, token)
	runners, _ := e.svcs.CIRunner.ListInstanceRunners(context.Background())
	var id int64
	for _, r := range runners {
		if len(r.Labels) == 1 && r.Labels[0] == e.label {
			id = r.ID
		}
	}
	if err := e.svcs.CIRunner.Revoke(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	wantCode(t, e.call(t, http.MethodPost, "/api/runner/claim", token, nil), http.StatusUnauthorized)
	wantCode(t, e.call(t, http.MethodPost, jobPath(spec.JobID, "heartbeat"), token, nil), http.StatusUnauthorized)
	wantCode(t, e.call(t, http.MethodPost, jobPath(spec.JobID, "finish"), token, runnerapi.FinishRequest{Result: "success"}), http.StatusUnauthorized)
}

func TestRunnerAPI_RunnerCannotTouchAnotherRunnersJob(t *testing.T) {
	e := newRunnerEnv(t)
	owner := e.registerRunner(t, nil)
	other := e.registerRunner(t, nil)
	spec := e.claim(t, owner)
	wantCode(t, e.call(t, http.MethodPost, jobPath(spec.JobID, "heartbeat"), other, nil), http.StatusForbidden)
	wantCode(t, e.call(t, http.MethodPost, jobPath(spec.JobID, "steps/0"), other, runnerapi.StepRequest{Status: "success"}), http.StatusForbidden)
	wantCode(t, e.call(t, http.MethodPost, jobPath(spec.JobID, "finish"), other, runnerapi.FinishRequest{Result: "success"}), http.StatusForbidden)
	if e.job(t, spec.JobID).Status != model.CIStatusRunning {
		t.Errorf("another runner changed the job")
	}
}

func TestRunnerAPI_JobWithNoMatchingRunnerStaysQueued(t *testing.T) {
	e := newRunnerEnv(t)
	token := e.registerRunner(t, nil, "other-"+e.label)
	if spec := e.claim(t, token); spec != nil {
		t.Fatalf("a runner without the label claimed %q", spec.Name)
	}
	jobs, _ := e.svcs.WorkflowRun.ListJobs(context.Background(), e.run(t).ID)
	if jobs[0].Status != model.CIStatusQueued {
		t.Errorf("job = %s, want queued", jobs[0].Status)
	}
}

func TestRunnerAPI_RepositoryRunnerClaimsOnlyItsRepo(t *testing.T) {
	e := newRunnerEnv(t)
	other := seedOwnedRepo(t, testutil.OpenTestDB(t), false)
	otherRepo, err := e.svcs.Repo.Get(context.Background(), other.owner.name, other.name)
	if err != nil {
		t.Fatal(err)
	}
	mine, err := e.svcs.Repo.Get(context.Background(), e.r.owner.name, e.r.name)
	if err != nil {
		t.Fatal(err)
	}
	foreign := e.registerRunner(t, otherRepo)
	if spec := e.claim(t, foreign); spec != nil {
		t.Fatalf("another repo's runner claimed %q", spec.Name)
	}
	own := e.registerRunner(t, mine)
	if spec := e.claim(t, own); spec == nil || spec.Name != "test" {
		t.Fatalf("own repo runner claim = %+v", spec)
	}
}

func TestRunnerAPI_HeartbeatTellsRunnerToStopWhenTheRunIsCancelled(t *testing.T) {
	e := newRunnerEnv(t)
	token := e.registerRunner(t, nil)
	spec := e.claim(t, token)
	rr := e.call(t, http.MethodPost, jobPath(spec.JobID, "heartbeat"), token, nil)
	if !decodeBody[runnerapi.HeartbeatResponse](t, rr).Continue {
		t.Fatal("a live job should continue")
	}
	repo, _ := e.svcs.Repo.Get(context.Background(), e.r.owner.name, e.r.name)
	if err := e.svcs.WorkflowRun.Cancel(context.Background(), repo, e.run(t).Number, e.r.owner.id); err != nil {
		t.Fatal(err)
	}
	rr = e.call(t, http.MethodPost, jobPath(spec.JobID, "heartbeat"), token, nil)
	wantCode(t, rr, http.StatusOK)
	if decodeBody[runnerapi.HeartbeatResponse](t, rr).Continue {
		t.Error("a cancelled job should not continue")
	}
}

func TestRunnerAPI_JobWithoutHeartbeatFailsAsRunnerLost(t *testing.T) {
	e := newRunnerEnv(t)
	db := testutil.OpenTestDB(t)
	token := e.registerRunner(t, nil)
	spec := e.claim(t, token)

	// A fresh lease is left alone.
	if n, err := e.svcs.WorkflowRun.SweepLostJobs(context.Background(), timeNow()); err != nil || e.job(t, spec.JobID).Status != model.CIStatusRunning {
		t.Fatalf("sweep took a live job: n=%d err=%v", n, err)
	}
	testutil.Exec(t, db, `UPDATE workflow_jobs SET lease_until = NOW() - interval '1 second' WHERE id = $1`, spec.JobID)
	n, err := e.svcs.WorkflowRun.SweepLostJobs(context.Background(), timeNow())
	if err != nil || n < 1 {
		t.Fatalf("sweep = %d, %v", n, err)
	}
	job := e.job(t, spec.JobID)
	if job.Status != model.CIStatusFailure || job.Message != "runner lost" {
		t.Errorf("job = %s %q", job.Status, job.Message)
	}
	if run := e.run(t); run.Status != model.CIStatusFailure {
		t.Errorf("run = %s, want failure", run.Status)
	}
	// The runner that comes back is told the job is gone.
	wantCode(t, e.call(t, http.MethodPost, jobPath(spec.JobID, "finish"), token, runnerapi.FinishRequest{Result: "success"}), http.StatusConflict)
}

func TestRunnerAPI_HeartbeatKeepsTheLeaseAlive(t *testing.T) {
	e := newRunnerEnv(t)
	db := testutil.OpenTestDB(t)
	token := e.registerRunner(t, nil)
	spec := e.claim(t, token)
	testutil.Exec(t, db, `UPDATE workflow_jobs SET lease_until = NOW() + interval '1 second' WHERE id = $1`, spec.JobID)
	wantCode(t, e.call(t, http.MethodPost, jobPath(spec.JobID, "heartbeat"), token, nil), http.StatusOK)
	var secs float64
	if err := db.QueryRow(`SELECT EXTRACT(EPOCH FROM lease_until - NOW()) FROM workflow_jobs WHERE id = $1`, spec.JobID).Scan(&secs); err != nil {
		t.Fatal(err)
	}
	if secs < 50 {
		t.Errorf("lease has %.0f s left after a heartbeat, want about 60", secs)
	}
}

func (e *runnerEnv) basic(method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(""))
	req.SetBasicAuth("cz-job", token)
	rr := httptest.NewRecorder()
	e.h.ServeHTTP(rr, req)
	return rr
}

func TestJobToken_ClonesItsRepoOnly(t *testing.T) {
	e := newRunnerEnv(t)
	token := e.registerRunner(t, nil)
	spec := e.claim(t, token)
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	auth := &githttp.BasicAuth{Username: "cz-job", Password: spec.Token}

	repo, err := gogit.Clone(memory.NewStorage(), nil, &gogit.CloneOptions{URL: srv.URL + e.r.path + ".git", Auth: auth, ReferenceName: mainRef, SingleBranch: true})
	if err != nil {
		t.Fatalf("clone own repo: %v", err)
	}
	if _, err := repo.CommitObject(plumbingHash(e.sha)); err != nil {
		t.Errorf("cloned repo lacks the run's commit: %v", err)
	}

	// The same token on another repo is refused as if it did not exist, public or not.
	other := seedOwnedRepo(t, testutil.OpenTestDB(t), false)
	if rr := e.basic(http.MethodGet, other.path+"/info/refs?service=git-upload-pack", spec.Token); rr.Code != http.StatusNotFound {
		t.Errorf("other repo info/refs = %d, want 404", rr.Code)
	}
	if rr := e.basic(http.MethodPost, other.path+"/git-upload-pack", spec.Token); rr.Code != http.StatusNotFound {
		t.Errorf("other repo upload-pack = %d, want 404", rr.Code)
	}
}

func TestJobToken_CannotPush(t *testing.T) {
	e := newRunnerEnv(t)
	spec := e.claim(t, e.registerRunner(t, nil))
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, e.r.path + "/info/refs?service=git-receive-pack"},
		{http.MethodPost, e.r.path + "/git-receive-pack"},
	} {
		if rr := e.basic(tc.method, tc.path, spec.Token); rr.Code != http.StatusForbidden {
			t.Errorf("%s %s = %d, want 403", tc.method, tc.path, rr.Code)
		}
	}
	// A real push, with a pack, through the git client.
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	err := e.r.git.Push(&gogit.PushOptions{RemoteURL: srv.URL + e.r.path + ".git", Auth: &githttp.BasicAuth{Username: "cz-job", Password: spec.Token},
		RefSpecs: []gogitconfig.RefSpec{"refs/heads/main:refs/heads/job-push"}})
	if err == nil {
		t.Error("push with a job token succeeded")
	}
	if _, err := e.r.git.Reference(plumbing.NewBranchReferenceName("job-push"), false); err == nil {
		t.Error("the push created a branch")
	}
}

func TestJobToken_ReachesNoUserAPI(t *testing.T) {
	e := newRunnerEnv(t)
	spec := e.claim(t, e.registerRunner(t, nil))
	testutil.Exec(t, testutil.OpenTestDB(t), `UPDATE repositories SET private = true WHERE id = $1`, e.r.id)
	for _, p := range []string{"/api/user/tokens", "/api/user/keys", "/api/repos/" + e.r.owner.name + "/" + e.r.name} {
		// A private repo's API answers an unauthenticated caller 404 rather than 401, so either is a refusal.
		if rr := e.call(t, http.MethodGet, p, spec.Token, nil); rr.Code != http.StatusUnauthorized && rr.Code != http.StatusNotFound {
			t.Errorf("GET %s with a job token = %d, want 401 or 404", p, rr.Code)
		}
	}
	// It is not a runner token either, so it cannot drive the runner API.
	wantCode(t, e.call(t, http.MethodPost, jobPath(spec.JobID, "finish"), spec.Token, runnerapi.FinishRequest{Result: "success"}), http.StatusUnauthorized)
}

func TestJobToken_DiesWithItsJob(t *testing.T) {
	e := newRunnerEnv(t)
	token := e.registerRunner(t, nil)
	spec := e.claim(t, token)
	info := e.r.path + "/info/refs?service=git-upload-pack"
	if rr := e.basic(http.MethodGet, info, spec.Token); rr.Code != http.StatusOK {
		t.Fatalf("running job's token = %d, want 200", rr.Code)
	}
	wantCode(t, e.call(t, http.MethodPost, jobPath(spec.JobID, "finish"), token, runnerapi.FinishRequest{Result: "success"}), http.StatusNoContent)
	if rr := e.basic(http.MethodGet, info, spec.Token); rr.Code != http.StatusUnauthorized {
		t.Errorf("finished job's token = %d, want 401", rr.Code)
	}
	if rr := e.basic(http.MethodGet, info, "czj_"+strings.Repeat("0", 64)); rr.Code != http.StatusUnauthorized {
		t.Errorf("unknown job token = %d, want 401", rr.Code)
	}
}

func timeNow() time.Time { return time.Now() }

func plumbingHash(s string) plumbing.Hash { return plumbing.NewHash(s) }

func TestRunnerClient_SpeaksTheProtocolTheServerServes(t *testing.T) {
	e := newRunnerEnv(t)
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	ctx := context.Background()

	reg, _ := e.svcs.CIRunner.CreateRegistrationToken(ctx, nil, e.r.owner.id)
	resp, err := runner.Register(ctx, srv.URL, reg, runnerapi.RegisterRequest{Name: "real-client", Labels: []string{e.label}})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	c := &runner.Client{Server: srv.URL, Token: resp.Token}
	spec, err := c.Claim(ctx, nil, 0)
	if err != nil || spec == nil || spec.Name != "test" {
		t.Fatalf("claim = %+v, %v", spec, err)
	}
	if cont, err := c.Heartbeat(ctx, spec.JobID); err != nil || !cont {
		t.Fatalf("heartbeat = %v, %v", cont, err)
	}
	if err := c.SetStep(ctx, spec.JobID, 0, "running"); err != nil {
		t.Fatalf("step: %v", err)
	}
	if err := c.Finish(ctx, spec.JobID, "success", ""); err != nil {
		t.Fatalf("finish: %v", err)
	}

	// The checkout a runner does with the per-job token works against this server.
	spec2, err := c.Claim(ctx, nil, 0)
	if err != nil || spec2 == nil {
		t.Fatalf("second claim = %+v, %v", spec2, err)
	}
	spec2.CloneURL = srv.URL + e.r.path + ".git"
	dir := t.TempDir()
	if err := runner.GitCheckout(ctx, spec2, dir); err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".cloudzilla", "workflows", "ci.yml")); err != nil {
		t.Errorf("checkout lacks the workflow file: %v", err)
	}

	runners, _ := e.svcs.CIRunner.ListInstanceRunners(ctx)
	for _, r := range runners {
		if r.Name == "real-client" {
			_ = e.svcs.CIRunner.Revoke(ctx, r.ID)
		}
	}
	if _, err := c.Claim(ctx, nil, 0); !errors.Is(err, runner.ErrUnauthorized) {
		t.Errorf("claim after revoke = %v, want ErrUnauthorized", err)
	}
}
