package service_test

import (
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

const forkPullWorkflow = "on: pull_request\njobs:\n  test:\n    runs-on: linux\n    steps:\n      - run: make test\n"

type forkCIEnv struct {
	*ciEnv
	fork    *model.Repository
	forker  ciUser
	forkSHA plumbing.Hash
	pull    *model.PullRequest
}

// newForkCIEnv forks the base as a second user, gives the fork a fix-x branch with a pull_request workflow and opens
// a pull request from it through the real services, so the Run is created the way production creates it.
func newForkCIEnv(t *testing.T) *forkCIEnv {
	t.Helper()
	e := &forkCIEnv{ciEnv: newCIEnv(t, nil)}
	e.commit(e.repo, "main", map[string]string{"a.txt": "a"})
	e.forker = e.user()
	fork, err := e.svcs.Repo.Fork(testCtx, e.repo.OwnerName, e.repo.Name, e.forker.id, e.forker.name, service.ForkOptions{})
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	e.fork = fork
	if err := e.svcs.Code.CreateBranch(fork.OwnerName, fork.Name, "fix-x", "main"); err != nil {
		t.Fatal(err)
	}
	e.forkSHA = e.commit(fork, "fix-x", map[string]string{ciPath: forkPullWorkflow})
	e.pull, err = e.svcs.Pull.CreateFromHead(testCtx, e.repo.OwnerName, e.repo.Name, fork.OwnerName+"/"+fork.Name,
		e.forker.id, "Fix x", "", "fix-x", "main", false, nil)
	if err != nil {
		t.Fatalf("open pull: %v", err)
	}
	return e
}

// waitForRuns waits for the background Run creation that opening a pull request starts.
func (e *forkCIEnv) waitForRuns(n int) []model.WorkflowRun {
	e.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if runs := e.runs(); len(runs) >= n || time.Now().After(deadline) {
			return runs
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestForkPull_CreatesAWaitingRunWithoutSecrets(t *testing.T) {
	e := newForkCIEnv(t)

	runs := e.waitForRuns(1)

	if len(runs) != 1 {
		t.Fatalf("want one Run, got %d", len(runs))
	}
	r := runs[0]
	if r.Status != model.CIStatusWaiting || !r.NoSecrets || r.SHA != e.forkSHA.String() || r.PullRequestID == nil || *r.PullRequestID != e.pull.ID {
		t.Errorf("run = %+v, want a waiting, no-secrets Run at the fork's commit for the pull request", r)
	}
}

func TestForkPull_JobTokenIsBoundToTheFork(t *testing.T) {
	e := newForkCIEnv(t)
	e.waitForRuns(1)
	if err := e.svcs.WorkflowRun.Approve(testCtx, e.repo, 1, e.owner.id); err != nil {
		t.Fatalf("approve: %v", err)
	}
	reg, err := e.svcs.CIRunner.CreateRegistrationToken(testCtx, nil, e.owner.id)
	if err != nil {
		t.Fatal(err)
	}
	runner, _, err := e.svcs.CIRunner.Register(testCtx, reg, "r1", []string{"linux"}, "test")
	if err != nil {
		t.Fatal(err)
	}

	spec, err := e.svcs.CIRunner.Claim(testCtx, runner, nil)
	if err != nil || spec == nil {
		t.Fatalf("claim: %v %v", spec, err)
	}

	if want := "http://cz.test/" + e.fork.OwnerName + "/" + e.fork.Name + ".git"; spec.CloneURL != want {
		t.Errorf("clone url = %q, want the fork's %q", spec.CloneURL, want)
	}
	jt, err := e.svcs.CIRunner.VerifyJobToken(testCtx, spec.Token)
	if err != nil {
		t.Fatal(err)
	}
	if jt.CloneRepoID != e.fork.ID {
		t.Errorf("token clones repo %d, want the fork %d", jt.CloneRepoID, e.fork.ID)
	}
	if jt.RepoID != e.repo.ID {
		t.Errorf("token's Run repo = %d, want the base %d", jt.RepoID, e.repo.ID)
	}
}

func TestForkPush_MakesANewWaitingRun(t *testing.T) {
	e := newForkCIEnv(t)
	e.waitForRuns(1)
	next := e.commit(e.fork, "fix-x", map[string]string{"b.txt": "b"})

	e.svcs.WorkflowRun.OnPush(testCtx, e.fork, e.forker.id, []*packp.Command{{Name: plumbing.NewBranchReferenceName("fix-x"), New: next}})

	runs := e.runs()
	if len(runs) != 2 || runs[0].SHA != next.String() || runs[0].Status != model.CIStatusWaiting || runs[0].Event != "pull_request" {
		t.Errorf("runs after a push to the fork's head branch: %+v", runs)
	}
}

func TestBasePush_DoesNotRunAForkPullWithTheSameBranchName(t *testing.T) {
	e := newForkCIEnv(t)
	e.waitForRuns(1)
	other := e.commit(e.repo, "fix-x", map[string]string{"unrelated.txt": "u"})

	e.svcs.WorkflowRun.OnPush(testCtx, e.repo, e.owner.id, []*packp.Command{{Name: plumbing.NewBranchReferenceName("fix-x"), New: other}})

	if runs := e.runs(); len(runs) != 1 {
		t.Errorf("a push to the base's own fix-x made %d Runs, want the fork pull's one", len(runs))
	}
}
