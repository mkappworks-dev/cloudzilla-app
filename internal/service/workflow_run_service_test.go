package service_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const ciWorkflow = `name: CI
on: push
jobs:
  test:
    runs-on: linux
    steps:
      - run: go test ./...
  lint:
    runs-on: linux
    steps:
      - run: golangci-lint run
  deploy:
    runs-on: linux
    needs: [test, lint]
    steps:
      - run: ./deploy.sh
  cleanup:
    runs-on: linux
    needs: [test]
    if: failure()
    steps:
      - run: ./cleanup.sh
`

const ciPath = ".cloudzilla/workflows/ci.yml"

type ciUser struct {
	id   int64
	name string
}

// ciEnv is a real database and a service stack over bare repos on disk.
type ciEnv struct {
	t     *testing.T
	db    *sql.DB
	svcs  *service.Services
	root  string
	repo  *model.Repository
	owner ciUser
}

func newCIEnv(t *testing.T, tune func(*config.CIConfig)) *ciEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	root := t.TempDir()
	ci := config.DefaultCI()
	if tune != nil {
		tune(&ci)
	}
	cfg := &config.Config{Server: config.ServerConfig{BaseURL: "http://cz.test"}, Git: config.GitConfig{ReposRoot: root}, CI: ci}
	e := &ciEnv{t: t, db: db, svcs: service.New(store.New(db), cfg), root: root}
	e.repo, e.owner = e.newRepo()
	return e
}

// newRepo seeds a user with an empty repository on disk.
func (e *ciEnv) newRepo() (*model.Repository, ciUser) {
	e.t.Helper()
	suffix := testutil.UniqueSuffix(e.t)
	u := ciUser{id: testutil.SeedUser(e.t, e.db, suffix), name: "testuser_" + suffix}
	id := testutil.SeedRepo(e.t, e.db, u.id, u.name, suffix)
	repo, err := store.NewRepoStore(e.db).GetByID(context.Background(), id)
	if err != nil {
		e.t.Fatalf("load repo: %v", err)
	}
	dir, err := service.RepoDir(e.root, repo.OwnerName, repo.Name+".git")
	if err != nil {
		e.t.Fatalf("repo dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		e.t.Fatalf("mkdir: %v", err)
	}
	testutil.InitBareRepo(e.t, dir)
	return repo, u
}

func (e *ciEnv) user() ciUser {
	e.t.Helper()
	suffix := testutil.UniqueSuffix(e.t)
	return ciUser{id: testutil.SeedUser(e.t, e.db, suffix), name: "testuser_" + suffix}
}

// commit puts files on branch of repo, one commit per file, and returns the tip.
func (e *ciEnv) commit(repo *model.Repository, branch string, files map[string]string) plumbing.Hash {
	e.t.Helper()
	var tip plumbing.Hash
	for p, content := range files {
		upd, err := e.svcs.Code.CommitFile(repo.OwnerName, repo.Name, branch, p, []byte(content),
			service.GitAuthor{Name: "T", Email: "t@test.invalid"}, "add "+p)
		if err != nil {
			e.t.Fatalf("commit %s: %v", p, err)
		}
		tip = upd.New
	}
	return tip
}

func (e *ciEnv) pushBranch(branch string, sha plumbing.Hash, userID int64) {
	e.t.Helper()
	e.svcs.WorkflowRun.OnPush(context.Background(), e.repo, userID, []*packp.Command{{
		Name: plumbing.NewBranchReferenceName(branch), New: sha}})
}

func (e *ciEnv) runs() []model.WorkflowRun {
	e.t.Helper()
	runs, err := e.svcs.WorkflowRun.ListRuns(context.Background(), e.repo.ID, 100)
	if err != nil {
		e.t.Fatalf("list runs: %v", err)
	}
	return runs
}

func (e *ciEnv) onlyRun() model.WorkflowRun {
	e.t.Helper()
	runs := e.runs()
	if len(runs) != 1 {
		e.t.Fatalf("got %d runs, want 1", len(runs))
	}
	return runs[0]
}

func (e *ciEnv) run(number int) model.WorkflowRun {
	e.t.Helper()
	r, err := e.svcs.WorkflowRun.GetRun(context.Background(), e.repo.ID, number)
	if err != nil {
		e.t.Fatalf("get run %d: %v", number, err)
	}
	return *r
}

// jobs maps each Job of the Run at its latest attempt, by name, to its status.
func (e *ciEnv) jobs(run model.WorkflowRun) map[string]model.CIStatus {
	e.t.Helper()
	jobs, err := e.svcs.WorkflowRun.ListJobs(context.Background(), run.ID)
	if err != nil {
		e.t.Fatalf("list jobs: %v", err)
	}
	out := map[string]model.CIStatus{}
	for _, j := range jobs {
		out[j.Name] = j.Status
	}
	return out
}

func (e *ciEnv) job(run model.WorkflowRun, name string) model.WorkflowJob {
	e.t.Helper()
	jobs, err := e.svcs.WorkflowRun.ListJobs(context.Background(), run.ID)
	if err != nil {
		e.t.Fatalf("list jobs: %v", err)
	}
	for _, j := range jobs {
		if j.Name == name {
			return j
		}
	}
	e.t.Fatalf("run %d has no job %q", run.Number, name)
	return model.WorkflowJob{}
}

func (e *ciEnv) finish(run model.WorkflowRun, name string, result model.CIStatus) {
	e.t.Helper()
	if err := e.svcs.WorkflowRun.FinishJob(context.Background(), e.job(run, name).ID, result, ""); err != nil {
		e.t.Fatalf("finish %s: %v", name, err)
	}
}

func (e *ciEnv) status(sha, context string) (model.CommitStatus, bool) {
	e.t.Helper()
	all, err := e.svcs.CommitStatus.List(testCtx, e.repo.OwnerName, e.repo.Name, sha)
	if err != nil {
		e.t.Fatalf("list statuses: %v", err)
	}
	for _, s := range all {
		if s.Context == context {
			return s, true
		}
	}
	return model.CommitStatus{}, false
}

var testCtx = context.Background()

func wantJobs(t *testing.T, got map[string]model.CIStatus, want map[string]model.CIStatus) {
	t.Helper()
	for name, w := range want {
		if got[name] != w {
			t.Errorf("job %s = %q, want %q (all: %v)", name, got[name], w, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d jobs %v, want %d", len(got), got, len(want))
	}
}

func TestWorkflowRun_PushCreatesRunJobsAndPendingStatuses(t *testing.T) {
	e := newCIEnv(t, nil)
	sha := e.commit(e.repo, "main", map[string]string{ciPath: ciWorkflow})

	e.pushBranch("main", sha, e.owner.id)

	run := e.onlyRun()
	if run.Number != 1 || run.Event != "push" || run.Ref != "refs/heads/main" || run.SHA != sha.String() ||
		run.Status != model.CIStatusQueued || run.WorkflowName != "CI" || run.NoSecrets {
		t.Errorf("run = %+v", run)
	}
	if run.TriggerUserID == nil || *run.TriggerUserID != e.owner.id {
		t.Errorf("trigger user = %v, want %d", run.TriggerUserID, e.owner.id)
	}
	wantJobs(t, e.jobs(run), map[string]model.CIStatus{
		"test": model.CIStatusQueued, "lint": model.CIStatusQueued, "deploy": model.CIStatusWaiting, "cleanup": model.CIStatusWaiting,
	})
	steps, _ := e.svcs.WorkflowRun.ListSteps(testCtx, e.job(run, "test").ID)
	if len(steps) != 1 || steps[0].Run != "go test ./..." || steps[0].Name != "Run go test ./..." {
		t.Errorf("steps = %+v", steps)
	}
	for _, name := range []string{"test", "lint", "deploy", "cleanup"} {
		cs, ok := e.status(sha.String(), "ci/"+name)
		if !ok || cs.State != model.CommitStatusPending || cs.RunID == nil || *cs.RunID != run.ID || cs.CreatorID != e.owner.id ||
			cs.TargetURL != "http://cz.test/"+e.repo.OwnerName+"/"+e.repo.Name+"/actions/runs/1" {
			t.Errorf("status ci/%s = %+v (found %v)", name, cs, ok)
		}
	}
}

func TestWorkflowRun_DeployKeyPushAttributesStatusesToRepoOwner(t *testing.T) {
	e := newCIEnv(t, nil)
	sha := e.commit(e.repo, "main", map[string]string{ciPath: ciWorkflow})

	e.pushBranch("main", sha, 0)

	run := e.onlyRun()
	if run.TriggerUserID != nil {
		t.Errorf("trigger user = %d, want none", *run.TriggerUserID)
	}
	if cs, _ := e.status(sha.String(), "ci/test"); cs.CreatorID != e.owner.id {
		t.Errorf("status creator = %d, want repo owner %d", cs.CreatorID, e.owner.id)
	}
}

func TestWorkflowRun_DeleteOnlyPushCreatesNone(t *testing.T) {
	e := newCIEnv(t, nil)
	sha := e.commit(e.repo, "main", map[string]string{ciPath: ciWorkflow})

	e.svcs.WorkflowRun.OnPush(testCtx, e.repo, e.owner.id, []*packp.Command{{Name: "refs/heads/main", Old: sha}})

	if n := len(e.runs()); n != 0 {
		t.Errorf("a delete-only push created %d runs", n)
	}
}

func TestWorkflowRun_PushFiltersAndTags(t *testing.T) {
	e := newCIEnv(t, nil)
	wf := "on:\n  push:\n    branches: [main]\n    tags: ['v*']\njobs:\n  build:\n    runs-on: linux\n    steps:\n      - run: make\n"
	sha := e.commit(e.repo, "main", map[string]string{ciPath: wf})

	e.pushBranch("topic", sha, e.owner.id)
	if n := len(e.runs()); n != 0 {
		t.Fatalf("a branch the filter excludes created %d runs", n)
	}
	e.svcs.WorkflowRun.OnPush(testCtx, e.repo, e.owner.id, []*packp.Command{{Name: "refs/tags/v1.0", New: sha}})

	if run := e.onlyRun(); run.Ref != "refs/tags/v1.0" {
		t.Errorf("run ref = %q", run.Ref)
	}
}

func TestWorkflowRun_YAMLErrorCreatesFailedRunWithOneFailingStatus(t *testing.T) {
	e := newCIEnv(t, nil)
	sha := e.commit(e.repo, "main", map[string]string{".cloudzilla/workflows/broken.yml": "on: push\njobs:\n  a:\n    runs-on: linux\n    steps:\n      - uses: actions/checkout@v4\n"})

	e.pushBranch("main", sha, e.owner.id)

	run := e.onlyRun()
	if run.Status != model.CIStatusFailure || !strings.Contains(run.Error, `broken.yml:6`) || !strings.Contains(run.Error, `"uses"`) {
		t.Errorf("run = %+v", run)
	}
	if n := len(e.jobs(run)); n != 0 {
		t.Errorf("failed run has %d jobs", n)
	}
	cs, ok := e.status(sha.String(), "ci/broken")
	if !ok || cs.State != model.CommitStatusFailure || !strings.Contains(cs.Description, "uses") {
		t.Errorf("status = %+v (found %v)", cs, ok)
	}
}

func TestWorkflowRun_SeveralFilesMakeSeparateRuns(t *testing.T) {
	e := newCIEnv(t, nil)
	other := "on: push\njobs:\n  fmt:\n    runs-on: linux\n    steps:\n      - run: gofmt -l .\n"
	sha := e.commit(e.repo, "main", map[string]string{ciPath: ciWorkflow, ".cloudzilla/workflows/lint.yaml": other, "README.md": "x"})

	e.pushBranch("main", sha, e.owner.id)

	if n := len(e.runs()); n != 2 {
		t.Errorf("got %d runs, want one per workflow file", n)
	}
}

func TestWorkflowRun_MatrixExpandsToJobs(t *testing.T) {
	e := newCIEnv(t, nil)
	wf := "on: push\njobs:\n  test:\n    runs-on: linux\n    strategy:\n      matrix:\n        go: ['1.22', '1.23']\n    steps:\n      - run: go test\n  publish:\n    runs-on: linux\n    needs: test\n    steps:\n      - run: echo done\n"
	sha := e.commit(e.repo, "main", map[string]string{ciPath: wf})
	e.pushBranch("main", sha, e.owner.id)
	run := e.onlyRun()

	wantJobs(t, e.jobs(run), map[string]model.CIStatus{
		"test (1.22)": model.CIStatusQueued, "test (1.23)": model.CIStatusQueued, "publish": model.CIStatusWaiting})
	if _, ok := e.status(sha.String(), "ci/test (1.22)"); !ok {
		t.Error("no status for the matrix combination")
	}

	e.finish(run, "test (1.22)", model.CIStatusSuccess)
	if got := e.jobs(run)["publish"]; got != model.CIStatusWaiting {
		t.Errorf("publish = %q while a matrix job is unfinished", got)
	}
	e.finish(run, "test (1.23)", model.CIStatusSuccess)
	if got := e.jobs(run)["publish"]; got != model.CIStatusQueued {
		t.Errorf("publish = %q after the whole matrix passed", got)
	}
}

func TestWorkflowRun_NeedsReleaseJobsAndIfSkipsThem(t *testing.T) {
	e := newCIEnv(t, nil)
	sha := e.commit(e.repo, "main", map[string]string{ciPath: ciWorkflow})
	e.pushBranch("main", sha, e.owner.id)
	run := e.onlyRun()

	e.finish(run, "test", model.CIStatusSuccess)
	if got := e.jobs(run); got["deploy"] != model.CIStatusWaiting || got["cleanup"] != model.CIStatusSkipped {
		t.Fatalf("after test: %v", got)
	}
	if cs, _ := e.status(sha.String(), "ci/cleanup"); cs.State != model.CommitStatusSuccess || cs.Description != "Skipped" {
		t.Errorf("skipped job status = %+v", cs)
	}
	e.finish(run, "lint", model.CIStatusSuccess)
	if got := e.jobs(run)["deploy"]; got != model.CIStatusQueued {
		t.Fatalf("deploy = %q once its needs finished", got)
	}
	if r := e.run(run.Number); r.Status != model.CIStatusRunning {
		t.Errorf("run = %q while deploy is unfinished", r.Status)
	}
	e.finish(run, "deploy", model.CIStatusSuccess)
	if r := e.run(run.Number); r.Status != model.CIStatusSuccess || r.FinishedAt == nil {
		t.Errorf("run = %q finished %v", r.Status, r.FinishedAt)
	}
	if cs, _ := e.status(sha.String(), "ci/deploy"); cs.State != model.CommitStatusSuccess {
		t.Errorf("deploy status = %q", cs.State)
	}
}

func TestWorkflowRun_FailureSkipsDeployAndRunsCleanup(t *testing.T) {
	e := newCIEnv(t, nil)
	sha := e.commit(e.repo, "main", map[string]string{ciPath: ciWorkflow})
	e.pushBranch("main", sha, e.owner.id)
	run := e.onlyRun()

	e.finish(run, "test", model.CIStatusFailure)
	e.finish(run, "lint", model.CIStatusSuccess)

	wantJobs(t, e.jobs(run), map[string]model.CIStatus{
		"test": model.CIStatusFailure, "lint": model.CIStatusSuccess, "deploy": model.CIStatusSkipped, "cleanup": model.CIStatusQueued})
	e.finish(run, "cleanup", model.CIStatusSuccess)
	if r := e.run(run.Number); r.Status != model.CIStatusFailure {
		t.Errorf("run = %q, want failure", r.Status)
	}
	if cs, _ := e.status(sha.String(), "ci/test"); cs.State != model.CommitStatusFailure {
		t.Errorf("test status = %q", cs.State)
	}
}

func TestWorkflowRun_RequiredCheckBlocksMergeUntilJobSucceeds(t *testing.T) {
	e := newCIEnv(t, nil)
	e.commit(e.repo, "main", map[string]string{"a.txt": "a"})
	sha := e.commit(e.repo, "feature", map[string]string{ciPath: "on: push\njobs:\n  test:\n    runs-on: linux\n    steps:\n      - run: make test\n"})
	e.pushBranch("feature", sha, e.owner.id)
	run := e.onlyRun()
	bp := &model.BranchProtection{RepoID: e.repo.ID, Pattern: "main", RequireStatusChecks: model.StringSlice{"ci/test"}}
	if err := e.svcs.BranchProtection.Create(testCtx, bp); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	pr := &model.PullRequest{ID: -1, BaseBranch: "main"}
	merge := func() error { return e.svcs.BranchProtection.CheckMerge(testCtx, e.repo.ID, pr, sha.String()) }

	if err := merge(); !errors.Is(err, service.ErrStatusCheckFailed) {
		t.Errorf("pending job: %v, want ErrStatusCheckFailed", err)
	}
	e.finish(run, "test", model.CIStatusFailure)
	if err := merge(); !errors.Is(err, service.ErrStatusCheckFailed) {
		t.Errorf("failed job: %v, want ErrStatusCheckFailed", err)
	}
	if err := e.svcs.WorkflowRun.Rerun(testCtx, e.repo, run.Number, e.owner.id, true); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	e.finish(run, "test", model.CIStatusSuccess)
	if err := merge(); err != nil {
		t.Errorf("successful job: %v", err)
	}
}

func TestWorkflowRun_BrokenWorkflowBlocksMergeToo(t *testing.T) {
	e := newCIEnv(t, nil)
	sha := e.commit(e.repo, "main", map[string]string{".cloudzilla/workflows/ci.yml": "on: push\njobs: {}\n"})
	e.pushBranch("main", sha, e.owner.id)
	bp := &model.BranchProtection{RepoID: e.repo.ID, Pattern: "main", RequireStatusChecks: model.StringSlice{"ci/ci"}}
	if err := e.svcs.BranchProtection.Create(testCtx, bp); err != nil {
		t.Fatalf("create rule: %v", err)
	}

	err := e.svcs.BranchProtection.CheckMerge(testCtx, e.repo.ID, &model.PullRequest{ID: -1, BaseBranch: "main"}, sha.String())

	if !errors.Is(err, service.ErrStatusCheckFailed) {
		t.Errorf("merge with a failing workflow status: %v", err)
	}
}

func TestWorkflowRun_NoRunsWhenCIOffArchivedOrDeleted(t *testing.T) {
	tests := []struct {
		name   string
		break_ func(e *ciEnv)
	}{
		{"repo CI switched off", func(e *ciEnv) {
			if err := e.svcs.WorkflowRun.SetRepoEnabled(testCtx, e.repo.ID, false); err != nil {
				t.Fatal(err)
			}
		}},
		{"archived", func(e *ciEnv) {
			testutil.Exec(t, e.db, `UPDATE repositories SET is_archived = true WHERE id = $1`, e.repo.ID)
		}},
		{"soft-deleted", func(e *ciEnv) {
			testutil.Exec(t, e.db, `UPDATE repositories SET deleted_at = NOW() WHERE id = $1`, e.repo.ID)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newCIEnv(t, nil)
			sha := e.commit(e.repo, "main", map[string]string{ciPath: ciWorkflow})
			tt.break_(e)

			e.pushBranch("main", sha, e.owner.id)

			var n int
			if err := e.db.QueryRow(`SELECT COUNT(*) FROM workflow_runs WHERE repo_id = $1`, e.repo.ID).Scan(&n); err != nil || n != 0 {
				t.Errorf("runs = %d (%v), want none", n, err)
			}
		})
	}
	t.Run("instance CI off", func(t *testing.T) {
		e := newCIEnv(t, func(c *config.CIConfig) { c.Enabled = false })
		sha := e.commit(e.repo, "main", map[string]string{ciPath: ciWorkflow})
		e.pushBranch("main", sha, e.owner.id)
		if n := len(e.runs()); n != 0 {
			t.Errorf("runs = %d, want none", n)
		}
	})
}

func TestWorkflowRun_CancelAndRerunKeepOldAttemptRows(t *testing.T) {
	e := newCIEnv(t, nil)
	sha := e.commit(e.repo, "main", map[string]string{ciPath: ciWorkflow})
	e.pushBranch("main", sha, e.owner.id)
	run := e.onlyRun()
	reader := e.user()

	if err := e.svcs.WorkflowRun.Cancel(testCtx, e.repo, run.Number, reader.id); !errors.Is(err, service.ErrWorkflowForbidden) {
		t.Fatalf("a reader cancelled a run: %v", err)
	}
	if err := e.svcs.WorkflowRun.Rerun(testCtx, e.repo, run.Number, e.owner.id, false); !errors.Is(err, service.ErrRunNotFinished) {
		t.Fatalf("re-run of an unfinished run: %v", err)
	}
	e.finish(run, "test", model.CIStatusFailure)
	if err := e.svcs.WorkflowRun.Cancel(testCtx, e.repo, run.Number, e.owner.id); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	r := e.run(run.Number)
	if r.Status != model.CIStatusCancelled || r.CancelledBy == nil || *r.CancelledBy != e.owner.id {
		t.Errorf("run after cancel = %+v", r)
	}
	wantJobs(t, e.jobs(run), map[string]model.CIStatus{
		"test": model.CIStatusFailure, "lint": model.CIStatusCancelled, "deploy": model.CIStatusCancelled, "cleanup": model.CIStatusCancelled})
	if cs, _ := e.status(sha.String(), "ci/lint"); cs.State != model.CommitStatusError || cs.Description != "Cancelled" {
		t.Errorf("cancelled job status = %+v", cs)
	}
	if err := e.svcs.WorkflowRun.Cancel(testCtx, e.repo, run.Number, e.owner.id); !errors.Is(err, service.ErrRunFinished) {
		t.Errorf("second cancel: %v", err)
	}

	if err := e.svcs.WorkflowRun.Rerun(testCtx, e.repo, run.Number, e.owner.id, true); err != nil {
		t.Fatalf("re-run failed jobs: %v", err)
	}
	r = e.run(run.Number)
	if r.Attempt != 2 || r.Status != model.CIStatusQueued || r.FinishedAt != nil || r.Number != run.Number {
		t.Errorf("run after re-run = %+v", r)
	}
	// Failed and cancelled jobs, and cleanup (needs test) run again; deploy needs test too.
	wantJobs(t, e.jobs(run), map[string]model.CIStatus{
		"test": model.CIStatusQueued, "lint": model.CIStatusQueued, "deploy": model.CIStatusWaiting, "cleanup": model.CIStatusWaiting})
	all, _ := e.svcs.WorkflowRun.ListAllJobs(testCtx, run.ID)
	if len(all) != 8 {
		t.Errorf("job rows = %d, want the 4 of attempt 1 and 4 of attempt 2", len(all))
	}
}

func TestWorkflowRun_RerunFailedOnlyKeepsPassedJobs(t *testing.T) {
	e := newCIEnv(t, nil)
	sha := e.commit(e.repo, "main", map[string]string{ciPath: ciWorkflow})
	e.pushBranch("main", sha, e.owner.id)
	run := e.onlyRun()
	e.finish(run, "test", model.CIStatusSuccess)
	e.finish(run, "lint", model.CIStatusFailure)
	if r := e.run(run.Number); r.Status != model.CIStatusFailure {
		t.Fatalf("run = %q", r.Status)
	}

	if err := e.svcs.WorkflowRun.Rerun(testCtx, e.repo, run.Number, e.owner.id, true); err != nil {
		t.Fatalf("rerun: %v", err)
	}

	wantJobs(t, e.jobs(run), map[string]model.CIStatus{
		"test": model.CIStatusSuccess, "lint": model.CIStatusQueued, "deploy": model.CIStatusWaiting, "cleanup": model.CIStatusSkipped})
	e.finish(run, "lint", model.CIStatusSuccess)
	if got := e.jobs(run)["deploy"]; got != model.CIStatusQueued {
		t.Errorf("deploy = %q after the failed job passed on attempt 2", got)
	}
}

func TestWorkflowRun_RerunUsesStoredFileNotBranchTip(t *testing.T) {
	e := newCIEnv(t, nil)
	sha := e.commit(e.repo, "main", map[string]string{ciPath: ciWorkflow})
	e.pushBranch("main", sha, e.owner.id)
	run := e.onlyRun()
	e.commit(e.repo, "main", map[string]string{ciPath: "on: push\njobs:\n  other:\n    runs-on: linux\n    steps:\n      - run: x\n"})
	if err := e.svcs.WorkflowRun.Cancel(testCtx, e.repo, run.Number, e.owner.id); err != nil {
		t.Fatal(err)
	}

	if err := e.svcs.WorkflowRun.Rerun(testCtx, e.repo, run.Number, e.owner.id, false); err != nil {
		t.Fatalf("rerun: %v", err)
	}

	if _, ok := e.jobs(run)["test"]; !ok {
		t.Errorf("re-run did not use the stored workflow: %v", e.jobs(run))
	}
}

func TestWorkflowRun_NewPushCancelsQueuedRunOfSameRef(t *testing.T) {
	e := newCIEnv(t, nil)
	first := e.commit(e.repo, "main", map[string]string{ciPath: ciWorkflow})
	e.pushBranch("main", first, e.owner.id)
	old := e.onlyRun()
	e.finish(old, "test", model.CIStatusSuccess) // running: not cancelled by the default rule

	second := e.commit(e.repo, "main", map[string]string{"x.txt": "x"})
	e.pushBranch("main", second, e.owner.id)
	if r := e.run(1); r.Status != model.CIStatusRunning {
		t.Fatalf("a running run was cancelled: %q", r.Status)
	}
	third := e.commit(e.repo, "main", map[string]string{"y.txt": "y"})
	e.pushBranch("main", third, e.owner.id)

	if r := e.run(2); r.Status != model.CIStatusCancelled {
		t.Errorf("queued run #2 = %q, want cancelled by the newer push", r.Status)
	}
	if r := e.run(3); r.Status != model.CIStatusQueued {
		t.Errorf("run #3 = %q", r.Status)
	}
	if cs, _ := e.status(second.String(), "ci/test"); cs.State != model.CommitStatusError {
		t.Errorf("cancelled run's status = %q", cs.State)
	}
}

func TestWorkflowRun_CancelInProgressGroupCancelsRunningRun(t *testing.T) {
	e := newCIEnv(t, nil)
	wf := "on: push\nconcurrency:\n  group: deploy-${{ github.ref_name }}\n  cancel-in-progress: true\njobs:\n  a:\n    runs-on: linux\n    steps:\n      - run: x\n  b:\n    runs-on: linux\n    needs: a\n    steps:\n      - run: y\n"
	first := e.commit(e.repo, "main", map[string]string{ciPath: wf})
	e.pushBranch("main", first, e.owner.id)
	e.finish(e.run(1), "a", model.CIStatusSuccess)
	e.pushBranch("other", first, e.owner.id)

	second := e.commit(e.repo, "main", map[string]string{"x.txt": "x"})
	e.pushBranch("main", second, e.owner.id)

	if r := e.run(1); r.Status != model.CIStatusCancelled || r.ConcurrencyGrp != "deploy-main" {
		t.Errorf("run #1 = %q group %q, want cancelled in group deploy-main", r.Status, r.ConcurrencyGrp)
	}
	if r := e.run(2); r.Status != model.CIStatusQueued || r.ConcurrencyGrp != "deploy-other" {
		t.Errorf("run #2 (other branch) = %q group %q", r.Status, r.ConcurrencyGrp)
	}
}

func TestWorkflowRun_QueuedCapCancelsOldest(t *testing.T) {
	e := newCIEnv(t, func(c *config.CIConfig) { c.MaxQueuedRunsPerRepo = 2 })
	one := "on: push\njobs:\n  a:\n    runs-on: linux\n    steps:\n      - run: x\n"
	for i, branch := range []string{"b1", "b2", "b3"} {
		sha := e.commit(e.repo, branch, map[string]string{ciPath: one})
		e.pushBranch(branch, sha, e.owner.id)
		_ = i
	}

	got := []model.CIStatus{e.run(1).Status, e.run(2).Status, e.run(3).Status}
	want := []model.CIStatus{model.CIStatusCancelled, model.CIStatusQueued, model.CIStatusQueued}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("run #%d = %q, want %q", i+1, got[i], want[i])
		}
	}
}

func TestWorkflowRun_ClaimHonoursLabelsAndConcurrentRunCap(t *testing.T) {
	e := newCIEnv(t, func(c *config.CIConfig) { c.MaxConcurrentRunsPerRepo = 1 })
	one := "on: push\njobs:\n  a:\n    runs-on: [linux, gpu]\n    steps:\n      - run: x\n"
	for _, branch := range []string{"b1", "b2"} {
		sha := e.commit(e.repo, branch, map[string]string{ciPath: one})
		e.pushBranch(branch, sha, e.owner.id)
	}
	svc := e.svcs.WorkflowRun

	if j, err := svc.ClaimJob(testCtx, []string{"linux"}, nil); err != nil || j != nil {
		t.Fatalf("a runner without the gpu label claimed %+v (%v)", j, err)
	}
	j, err := svc.ClaimJob(testCtx, []string{"linux", "gpu"}, nil)
	if err != nil || j == nil || j.Status != model.CIStatusRunning {
		t.Fatalf("claim = %+v (%v)", j, err)
	}
	if r := e.run(1); r.Status != model.CIStatusRunning || r.StartedAt == nil {
		t.Errorf("run #1 = %q", r.Status)
	}
	if cs, _ := e.status(e.run(1).SHA, "ci/a"); cs.Description != "Running" {
		t.Errorf("claimed job status = %+v", cs)
	}
	if j2, err := svc.ClaimJob(testCtx, []string{"linux", "gpu"}, nil); err != nil || j2 != nil {
		t.Fatalf("second run started past the concurrent run cap: %+v (%v)", j2, err)
	}
	if err := svc.FinishJob(testCtx, j.ID, model.CIStatusSuccess, ""); err != nil {
		t.Fatal(err)
	}
	if j2, err := svc.ClaimJob(testCtx, []string{"linux", "gpu"}, nil); err != nil || j2 == nil {
		t.Fatalf("second run still blocked after the first finished: %+v (%v)", j2, err)
	}
}

func TestWorkflowRun_SweepCancelsJobQueuedOverADay(t *testing.T) {
	e := newCIEnv(t, nil)
	sha := e.commit(e.repo, "main", map[string]string{ciPath: ciWorkflow})
	e.pushBranch("main", sha, e.owner.id)
	run := e.onlyRun()

	if n, err := e.svcs.WorkflowRun.SweepUnclaimed(testCtx, time.Now().Add(23*time.Hour)); err != nil || n != 0 {
		t.Fatalf("sweep at 23h cancelled %d (%v)", n, err)
	}
	n, err := e.svcs.WorkflowRun.SweepUnclaimed(testCtx, time.Now().Add(25*time.Hour))
	if err != nil || n != 2 {
		t.Fatalf("sweep at 25h cancelled %d (%v), want the 2 queued jobs", n, err)
	}

	if got := e.jobs(run); got["test"] != model.CIStatusCancelled || got["deploy"] != model.CIStatusSkipped {
		t.Errorf("jobs = %v", got)
	}
	if j := e.job(run, "test"); j.Message != "no runner" {
		t.Errorf("message = %q", j.Message)
	}
	if r := e.run(run.Number); r.Status != model.CIStatusCancelled {
		t.Errorf("run = %q", r.Status)
	}
}

func TestWorkflowRun_DispatchNeedsWriteAndTrigger(t *testing.T) {
	e := newCIEnv(t, nil)
	manual := "on: workflow_dispatch\njobs:\n  a:\n    runs-on: linux\n    steps:\n      - run: x\n"
	e.commit(e.repo, "main", map[string]string{ciPath: manual, ".cloudzilla/workflows/push.yml": "on: push\njobs:\n  a:\n    runs-on: linux\n    steps:\n      - run: x\n"})

	if _, err := e.svcs.WorkflowRun.Dispatch(testCtx, e.repo, e.user().id, ciPath, "main"); !errors.Is(err, service.ErrWorkflowForbidden) {
		t.Errorf("a reader dispatched: %v", err)
	}
	if _, err := e.svcs.WorkflowRun.Dispatch(testCtx, e.repo, e.owner.id, ".cloudzilla/workflows/push.yml", "main"); !errors.Is(err, service.ErrWorkflowNotFound) {
		t.Errorf("dispatch of a workflow without the trigger: %v", err)
	}
	run, err := e.svcs.WorkflowRun.Dispatch(testCtx, e.repo, e.owner.id, ciPath, "main")
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if run.Event != "workflow_dispatch" || run.Ref != "refs/heads/main" || run.Status != model.CIStatusQueued {
		t.Errorf("run = %+v", run)
	}
}

func TestWorkflowRun_ForkPullRequestWaitsForApproval(t *testing.T) {
	e := newCIEnv(t, nil)
	e.commit(e.repo, "main", map[string]string{"a.txt": "a"})
	fork, forkOwner := e.newRepo()
	pullWf := "on: pull_request\njobs:\n  test:\n    runs-on: linux\n    steps:\n      - run: make test\n"
	head1 := e.commit(fork, "feature", map[string]string{ciPath: pullWf})
	pr := &model.PullRequest{RepoID: e.repo.ID, AuthorID: forkOwner.id, Title: "t", State: model.PRStateOpen, HeadBranch: "feature", BaseBranch: "main"}
	if err := store.NewPullStore(e.db).Create(testCtx, pr); err != nil {
		t.Fatalf("create pr: %v", err)
	}
	svc := e.svcs.WorkflowRun

	svc.OnPullRequest(testCtx, e.repo, fork, pr, "opened", forkOwner.id)

	run := e.onlyRun()
	if run.Status != model.CIStatusWaiting || !run.NoSecrets || run.SHA != head1.String() || run.PullRequestID == nil || *run.PullRequestID != pr.ID {
		t.Fatalf("run = %+v", run)
	}
	wantJobs(t, e.jobs(run), map[string]model.CIStatus{"test": model.CIStatusWaiting})
	if cs, _ := e.status(head1.String(), "ci/test"); cs.State != model.CommitStatusPending || cs.Description != "Approval required" {
		t.Errorf("status = %+v", cs)
	}
	if j, err := svc.ClaimJob(testCtx, []string{"linux"}, nil); err != nil || j != nil {
		t.Fatalf("a waiting run's job was claimed: %+v (%v)", j, err)
	}

	head2 := e.commit(fork, "feature", map[string]string{"more.txt": "m"})
	pr.HeadSHA = head2.String()
	svc.OnPullRequest(testCtx, e.repo, fork, pr, "synchronize", forkOwner.id)
	if runs := e.runs(); len(runs) != 2 || runs[0].Status != model.CIStatusWaiting || runs[1].Status != model.CIStatusWaiting {
		t.Fatalf("a second push should leave two waiting runs: %+v", runs)
	}

	if err := svc.Approve(testCtx, e.repo, 1, e.user().id); !errors.Is(err, service.ErrWorkflowForbidden) {
		t.Fatalf("a reader approved: %v", err)
	}
	if err := svc.Approve(testCtx, e.repo, 1, e.owner.id); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if r := e.run(1); r.Status != model.CIStatusQueued || !r.NoSecrets {
		t.Errorf("approved run = %+v", r)
	}
	wantJobs(t, e.jobs(e.run(1)), map[string]model.CIStatus{"test": model.CIStatusQueued})
	if r := e.run(2); r.Status != model.CIStatusWaiting {
		t.Errorf("run #2 = %q: approving run #1 must not approve it", r.Status)
	}
	if err := svc.Approve(testCtx, e.repo, 1, e.owner.id); !errors.Is(err, service.ErrRunNotWaiting) {
		t.Errorf("second approve: %v", err)
	}
}

func TestWorkflowRun_SamePullRequestFromBranchRunsAtOnce(t *testing.T) {
	e := newCIEnv(t, nil)
	e.commit(e.repo, "main", map[string]string{"a.txt": "a"})
	head := e.commit(e.repo, "feature", map[string]string{ciPath: "on: pull_request\njobs:\n  test:\n    runs-on: linux\n    steps:\n      - run: x\n"})
	pr := &model.PullRequest{RepoID: e.repo.ID, AuthorID: e.owner.id, Title: "t", State: model.PRStateOpen, HeadBranch: "feature", BaseBranch: "main"}
	if err := store.NewPullStore(e.db).Create(testCtx, pr); err != nil {
		t.Fatal(err)
	}

	e.svcs.WorkflowRun.OnPullRequest(testCtx, e.repo, nil, pr, "opened", e.owner.id)

	run := e.onlyRun()
	if run.Status != model.CIStatusQueued || run.NoSecrets || run.SHA != head.String() {
		t.Errorf("run = %+v", run)
	}
	// A push to the head branch makes a new run for the open pull request.
	next := e.commit(e.repo, "feature", map[string]string{"b.txt": "b"})
	e.pushBranch("feature", next, e.owner.id)
	if runs := e.runs(); len(runs) != 2 || runs[0].SHA != next.String() || runs[0].Event != "pull_request" {
		t.Errorf("runs after a push to the head branch: %+v", runs)
	}
}

func TestWorkflowRun_LimitsFailTheRunWithAMessage(t *testing.T) {
	e := newCIEnv(t, func(c *config.CIConfig) { c.MaxJobsPerRun = 2 })
	wf := "on: push\njobs:\n  a:\n    runs-on: x\n    strategy:\n      matrix:\n        n: [1, 2, 3]\n    steps:\n      - run: x\n"
	sha := e.commit(e.repo, "main", map[string]string{ciPath: wf})

	e.pushBranch("main", sha, e.owner.id)

	if run := e.onlyRun(); run.Status != model.CIStatusFailure || !strings.Contains(run.Error, "job limit exceeded") {
		t.Errorf("run = %+v", run)
	}
}

func TestWorkflowRun_PullServiceOpenAndReopenCreateRuns(t *testing.T) {
	e := newCIEnv(t, nil)
	e.commit(e.repo, "main", map[string]string{"a.txt": "a"})
	e.commit(e.repo, "feature", map[string]string{ciPath: "on:\n  pull_request:\n    types: [opened, reopened]\njobs:\n  test:\n    runs-on: linux\n    steps:\n      - run: x\n"})
	waitRuns := func(n int) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); len(e.runs()) != n; time.Sleep(20 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("got %d runs, want %d", len(e.runs()), n)
			}
		}
	}

	pr, err := e.svcs.Pull.Create(testCtx, e.repo.OwnerName, e.repo.Name, e.owner.id, "t", "", "feature", "main", false, nil)
	if err != nil {
		t.Fatalf("create pr: %v", err)
	}
	waitRuns(1)
	if _, err := e.svcs.Pull.SetState(testCtx, e.repo.OwnerName, e.repo.Name, pr.Number, model.PRStateClosed); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svcs.Pull.SetState(testCtx, e.repo.OwnerName, e.repo.Name, pr.Number, model.PRStateOpen); err != nil {
		t.Fatal(err)
	}

	waitRuns(2)
	if r := e.runs()[0]; r.Event != "pull_request" || r.PullRequestID == nil || *r.PullRequestID != pr.ID {
		t.Errorf("reopen run = %+v", r)
	}
}
