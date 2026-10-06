package ssh

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
	gossh "golang.org/x/crypto/ssh"
)

func (r pushRepo) seedIssues(t *testing.T, n int) {
	t.Helper()
	var ownerID int64
	if err := r.db.QueryRow(`SELECT owner_id FROM repositories WHERE id = $1`, r.id).Scan(&ownerID); err != nil {
		t.Fatalf("load owner: %v", err)
	}
	for i := 1; i <= n; i++ {
		testutil.Exec(t, r.db, `INSERT INTO issues (repo_id, number, author_id, title, body, state) VALUES ($1, $2, $3, 'i', '', 'open')`, r.id, i, ownerID)
	}
}

func (r pushRepo) issueState(t *testing.T, number int) string {
	t.Helper()
	var state string
	if err := r.db.QueryRow(`SELECT state FROM issues WHERE repo_id = $1 AND number = $2`, r.id, number).Scan(&state); err != nil {
		t.Fatalf("issue %d state: %v", number, err)
	}
	return state
}

// awaitClosed waits for issue number to close: the closer runs after the push
// has been answered.
func (r pushRepo) awaitClosed(t *testing.T, number int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for r.issueState(t, number) != "closed" {
		if time.Now().After(deadline) {
			t.Fatalf("issue #%d still open 5s after the push", number)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (r pushRepo) push(t *testing.T, cmds ...*packp.Command) {
	t.Helper()
	got := r.receivePack(t, cmds...)
	if got.status != 0 {
		t.Fatalf("push: exit %d, stderr %q", got.status, got.stderr)
	}
	for ref, status := range reportedRefs(t, got.stdout) {
		if status != "ok" {
			t.Fatalf("push %s: %s", ref, status)
		}
	}
}

func TestReceivePack_FastForwardToDefaultBranch_ClosesIssues(t *testing.T) {
	r := seedPushRepo(t)
	r.seedIssues(t, 3)
	first := testutil.WriteCommit(t, r.git.Storer, "Fixes #1", r.mainTip)
	second := testutil.WriteCommit(t, r.git.Storer, "tidy up\n\ncloses #2, resolves #1", first)

	r.push(t, &packp.Command{Name: mainRef, Old: r.mainTip, New: second})

	r.awaitClosed(t, 1)
	r.awaitClosed(t, 2)
	if got := r.issueState(t, 3); got != "open" {
		t.Errorf("issue #3 = %q, want open", got)
	}
	var sha string
	if err := r.db.QueryRow(
		`SELECT e.commit_sha FROM issue_events e JOIN issues i ON i.id = e.issue_id WHERE i.repo_id = $1 AND i.number = 1`, r.id,
	).Scan(&sha); err != nil {
		t.Fatalf("issue #1 event: %v", err)
	}
	if sha != first.String() {
		t.Errorf("issue #1 closed by %s, want the oldest commit naming it, %s", sha, first)
	}
}

// Each case pushes its command along with a fast-forward of main that closes
// #9: the closer handles a push's commands in order, so once #9 closes, the
// case's command has been passed over.
func TestReceivePack_OtherPushes_CloseNothing(t *testing.T) {
	tests := []struct {
		name string
		cmd  func(r pushRepo) *packp.Command
	}{
		{"another branch", func(r pushRepo) *packp.Command {
			return &packp.Command{Name: featureRef, Old: r.featureTip, New: testutil.WriteCommit(t, r.git.Storer, "Fixes #1", r.featureTip)}
		}},
		{"branch creation", func(r pushRepo) *packp.Command {
			return &packp.Command{Name: plumbing.NewBranchReferenceName("new"), New: testutil.WriteCommit(t, r.git.Storer, "Fixes #1", r.featureTip)}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := seedPushRepo(t)
			r.seedIssues(t, 9)
			sentinel := testutil.WriteCommit(t, r.git.Storer, "Fixes #9", r.mainTip)

			r.push(t, tc.cmd(r), &packp.Command{Name: mainRef, Old: r.mainTip, New: sentinel})

			r.awaitClosed(t, 9)
			if got := r.issueState(t, 1); got != "open" {
				t.Errorf("issue #1 = %q, want open", got)
			}
		})
	}
}

func TestReceivePack_ForcePushToDefaultBranch_ClosesNothing(t *testing.T) {
	r := seedPushRepo(t)
	r.seedIssues(t, 1)
	rewritten := testutil.WriteCommit(t, r.git.Storer, "Fixes #1", r.featureTip)

	r.push(t, &packp.Command{Name: mainRef, Old: r.mainTip, New: rewritten})

	assertStaysOpen(t, r.db, r.id, 1)
}

func TestReceivePack_DeployKeyPush_ClosesNothing(t *testing.T) {
	r := seedPushRepo(t)
	r.seedIssues(t, 1)
	cfg := &config.Config{Git: config.GitConfig{ReposRoot: r.reposRoot, SSHHostKey: filepath.Join(t.TempDir(), "host_key")}}
	deployKey := newKey(t)
	if _, err := service.New(store.New(r.db), cfg).DeployKey.Add(context.Background(), r.id, "d", string(gossh.MarshalAuthorizedKey(deployKey.PublicKey())), false); err != nil {
		t.Fatalf("add deploy key: %v", err)
	}
	r.key = deployKey
	fix := testutil.WriteCommit(t, r.git.Storer, "Fixes #1", r.mainTip)

	r.push(t, &packp.Command{Name: mainRef, Old: r.mainTip, New: fix})

	assertRef(t, r, mainRef, fix)
	assertStaysOpen(t, r.db, r.id, 1)
}

// assertStaysOpen checks issue number is still open a while after a push. With
// no later effect of the same closer run to wait for, it can only wait.
func assertStaysOpen(t *testing.T, db *sql.DB, repoID int64, number int) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		var state string
		if err := db.QueryRow(`SELECT state FROM issues WHERE repo_id = $1 AND number = $2`, repoID, number).Scan(&state); err != nil {
			t.Fatalf("issue state: %v", err)
		}
		if state != "open" {
			t.Fatalf("issue #%d = %q, want open", number, state)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
