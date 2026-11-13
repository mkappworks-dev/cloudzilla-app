package handler_test

import (
	"context"
	"database/sql"
	"net/http"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/router"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const pushedWorkflow = "name: CI\non: push\njobs:\n  test:\n    runs-on: linux\n    steps:\n      - run: go test ./...\n  build:\n    runs-on: linux\n    needs: test\n    steps:\n      - run: go build ./...\n"

type ciPushEnv struct {
	db    *sql.DB
	h     http.Handler
	svcs  *service.Services
	r     raceRepo
	pushs atomic.Int32
}

func newCIPushEnv(t *testing.T) *ciPushEnv { return newCIPushEnvWith(t, nil) }

func newCIPushEnvWith(t *testing.T, tweak func(*config.Config)) *ciPushEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "http://localhost:8080"},
		Auth:   config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName},
		Git:    config.GitConfig{ReposRoot: reposRoot},
		CI:     config.DefaultCI(),
	}
	if tweak != nil {
		tweak(cfg)
	}
	svcs := service.New(store.New(db), cfg)
	h, err := router.New(svcs, cfg, fstest.MapFS{})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	e := &ciPushEnv{db: db, h: h, svcs: svcs, r: seedRaceRepo(t, db, reposRoot)}
	svcs.WorkflowRun.WithPushDone(func() { e.pushs.Add(1) })
	return e
}

// push sends cmds with c's pack through git-receive-pack and waits for the Runs the push creates.
func (e *ciPushEnv) push(t *testing.T, c *testutil.PushClient, cmds ...*packp.Command) {
	t.Helper()
	before := e.pushs.Load()
	for ref, status := range receivePackOf(t, e.h, e.r, c.Pack(), cmds...) {
		if status != "ok" {
			t.Fatalf("push of %s: %q", ref, status)
		}
	}
	eventually(t, "the runs after the push", func() bool { return e.pushs.Load() > before })
}

func (e *ciPushEnv) runs(t *testing.T) []model.WorkflowRun {
	t.Helper()
	runs, err := e.svcs.WorkflowRun.ListRuns(context.Background(), e.r.id, 50)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	return runs
}

func TestGitPush_WithWorkflowCreatesRunJobsAndPendingStatuses(t *testing.T) {
	e := newCIPushEnv(t)
	c := testutil.NewPushClient(t, e.r.git)
	sha := c.Commit(e.r.mainTip, map[string]string{".cloudzilla/workflows/ci.yml": pushedWorkflow}, "add ci")

	e.push(t, c, &packp.Command{Name: mainRef, Old: e.r.mainTip, New: sha})

	runs := e.runs(t)
	if len(runs) != 1 || runs[0].Status != model.CIStatusQueued || runs[0].SHA != sha.String() || runs[0].Ref != string(mainRef) {
		t.Fatalf("runs = %+v", runs)
	}
	jobs, _ := e.svcs.WorkflowRun.ListJobs(context.Background(), runs[0].ID)
	if len(jobs) != 2 || jobs[0].Name != "test" || jobs[0].Status != model.CIStatusQueued || jobs[1].Status != model.CIStatusWaiting {
		t.Errorf("jobs = %+v", jobs)
	}
	statuses, err := e.svcs.CommitStatus.List(context.Background(), e.r.owner.name, e.r.name, sha.String())
	if err != nil || len(statuses) != 2 {
		t.Fatalf("statuses = %+v (%v)", statuses, err)
	}
	for _, cs := range statuses {
		if cs.State != model.CommitStatusPending || cs.CreatorID != e.r.owner.id || cs.RunID == nil || *cs.RunID != runs[0].ID {
			t.Errorf("status = %+v", cs)
		}
	}
}

func TestGitPush_DeleteOnlyCreatesNoRun(t *testing.T) {
	e := newCIPushEnv(t)
	c := testutil.NewPushClient(t, e.r.git)
	sha := c.Commit(e.r.mainTip, map[string]string{".cloudzilla/workflows/ci.yml": pushedWorkflow}, "add ci")
	e.push(t, c, &packp.Command{Name: plumbing.NewBranchReferenceName("topic"), New: sha})
	if n := len(e.runs(t)); n != 1 {
		t.Fatalf("creating the branch made %d runs, want 1", n)
	}

	e.push(t, c, &packp.Command{Name: plumbing.NewBranchReferenceName("topic"), Old: sha})

	if n := len(e.runs(t)); n != 1 {
		t.Errorf("a delete-only push made a run: %d runs", n)
	}
}

func TestGitPush_WorkflowWithYAMLErrorCreatesFailedRun(t *testing.T) {
	e := newCIPushEnv(t)
	c := testutil.NewPushClient(t, e.r.git)
	sha := c.Commit(e.r.mainTip, map[string]string{".cloudzilla/workflows/ci.yml": "on: push\njobs: [\n"}, "broken")

	e.push(t, c, &packp.Command{Name: mainRef, Old: e.r.mainTip, New: sha})

	runs := e.runs(t)
	if len(runs) != 1 || runs[0].Status != model.CIStatusFailure || runs[0].Error == "" {
		t.Fatalf("runs = %+v", runs)
	}
	statuses, _ := e.svcs.CommitStatus.List(context.Background(), e.r.owner.name, e.r.name, sha.String())
	if len(statuses) != 1 || statuses[0].Context != "ci/ci" || statuses[0].State != model.CommitStatusFailure {
		t.Errorf("statuses = %+v", statuses)
	}
}

func TestMergePull_CreatesPushRunForTheNewBaseTip(t *testing.T) {
	for _, strategy := range []string{"merge", "squash"} {
		t.Run(strategy, func(t *testing.T) {
			e := newCIPushEnv(t)
			code := service.NewCodeService(config.GitConfig{ReposRoot: e.r.gitDir[:len(e.r.gitDir)-len(e.r.owner.name+"/"+e.r.name+".git")-1]})
			if _, err := code.CommitFile(e.r.owner.name, e.r.name, "feature", ".cloudzilla/workflows/ci.yml", []byte(pushedWorkflow), raceAuthor, "add ci"); err != nil {
				t.Fatalf("commit workflow: %v", err)
			}
			seedOpenPull(t, e.db, e.r.seededRepo)
			before := e.pushs.Load()

			rr := requestAPIBody(e.h, http.MethodPatch, "/api/repos"+e.r.path+"/pulls/1", e.r.owner.token,
				`{"state":"merged","merge_strategy":"`+strategy+`"}`)
			if rr.Code != http.StatusOK {
				t.Fatalf("merge: %d %s", rr.Code, rr.Body.String())
			}
			eventually(t, "the push run after the merge", func() bool { return e.pushs.Load() > before })

			runs := e.runs(t)
			tip := branchHash(t, e.r.git, "main")
			if len(runs) != 1 || runs[0].Event != model.RunEventPush || runs[0].Ref != string(mainRef) || runs[0].SHA != tip.String() {
				t.Fatalf("runs = %+v, want one push run on main at %s", runs, tip)
			}
		})
	}
}
