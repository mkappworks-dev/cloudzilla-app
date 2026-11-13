package ssh

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	gossh "golang.org/x/crypto/ssh"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/secretscan"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const awsKey = "AKIAIOSFODNN7EXAMPLE"

type sshPushEnv struct {
	addr    string
	key     gossh.Signer
	svcs    *service.Services
	repo    *gogit.Repository
	repoID  int64
	userID  int64
	command string
	path    string
	base    plumbing.Hash
	scans   atomic.Int32
}

func newSSHPushEnv(t *testing.T, protection bool) *sshPushEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	root := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "http://localhost:8080"},
		Git:    config.GitConfig{ReposRoot: root, SSHHostKey: filepath.Join(t.TempDir(), "host_key")},
		SecretScanning: config.SecretScanningConfig{
			PushProtection: protection, MaxBlobBytes: 1 << 20, PushTimeout: 10 * time.Second, MaxBlobs: 5000,
		},
	}
	svcs := service.New(store.New(db), cfg)
	sfx := testutil.UniqueSuffix(t)
	owner := "testuser_p_" + sfx
	userID := testutil.SeedUser(t, db, "p_"+sfx)
	name := "testrepo_p_" + sfx
	repoID := testutil.SeedRepo(t, db, userID, owner, "p_"+sfx)
	repo, err := gogit.PlainInit(filepath.Join(root, owner, name+".git"), true)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	base := testutil.WriteCommit(t, repo.Storer, "base")
	main := plumbing.NewBranchReferenceName("main")
	if err := repo.Storer.SetReference(plumbing.NewHashReference(main, base)); err != nil {
		t.Fatalf("set main: %v", err)
	}
	key := newKey(t)
	if _, err := svcs.SSHKey.AddKey(context.Background(), userID, "k", string(gossh.MarshalAuthorizedKey(key.PublicKey()))); err != nil {
		t.Fatalf("add key: %v", err)
	}
	e := &sshPushEnv{
		addr: serve(t, New(cfg.Git, svcs)), key: key, svcs: svcs, repo: repo, repoID: repoID, userID: userID, base: base,
		command: "git-receive-pack '/" + owner + "/" + name + ".git'", path: "/" + owner + "/" + name,
	}
	svcs.PushScan.WithScanner(func(path string, content []byte, max int) ([]secretscan.Finding, error) {
		e.scans.Add(1)
		return secretscan.Scan(path, content, max), nil
	})
	return e
}

// push sends cmds and the client's pack, and returns the status of each ref.
func (e *sshPushEnv) push(t *testing.T, c *testutil.PushClient, cmds ...*packp.Command) map[plumbing.ReferenceName]string {
	t.Helper()
	got := runSSH(t, e.addr, e.key, e.command, string(c.Request(cmds...)))
	if got.status != 0 {
		t.Fatalf("exit %d, stderr %q", got.status, got.stderr)
	}
	out := bytes.NewReader([]byte(got.stdout))
	if err := packp.NewAdvRefs().Decode(out); err != nil {
		t.Fatalf("decode advertisement: %v", err)
	}
	status := packp.NewReportStatus()
	if err := status.Decode(out); err != nil {
		t.Fatalf("decode report status: %v", err)
	}
	refs := make(map[plumbing.ReferenceName]string)
	for _, cs := range status.CommandStatuses {
		refs[cs.ReferenceName] = cs.Status
	}
	return refs
}

func (e *sshPushEnv) wantRefusal(number string) string {
	return "secret detected: aws_access_key_id in config/deploy.env:1; if it is not a live secret, a writer can allow it at " +
		"http://localhost:8080" + e.path + "/security/secrets/" + number
}

func (e *sshPushEnv) alerts(t *testing.T) []model.SecretScanAlert {
	t.Helper()
	got, _, err := e.svcs.SecretAlert.List(context.Background(), e.repoID, "", "", 1)
	if err != nil {
		t.Fatalf("list alerts: %v", err)
	}
	return got
}

func keyFile() map[string]string {
	return map[string]string{"config/deploy.env": "AWS_ACCESS_KEY_ID=" + awsKey + "\n"}
}

func create(name string, to plumbing.Hash) *packp.Command {
	return &packp.Command{Name: plumbing.NewBranchReferenceName(name), New: to}
}

func TestPushProtection_SSH_NewBranchWithKey_Refused(t *testing.T) {
	e := newSSHPushEnv(t, true)
	c := testutil.NewPushClient(t, e.repo)
	bad := c.Commit(e.base, keyFile(), "add key")

	refs := e.push(t, c, create("topic", bad), create("topic2", bad))

	for _, name := range []string{"topic", "topic2"} {
		if got, want := refs[plumbing.NewBranchReferenceName(name)], e.wantRefusal("1"); got != want {
			t.Errorf("%s status = %q, want %q", name, got, want)
		}
		if _, err := e.repo.Reference(plumbing.NewBranchReferenceName(name), true); !errors.Is(err, plumbing.ErrReferenceNotFound) {
			t.Errorf("the refused ref %s exists: %v", name, err)
		}
	}
	if n := e.scans.Load(); n != 1 {
		t.Errorf("scanner ran %d times, want once for the one changed file", n)
	}
	if n := len(e.alerts(t)); n != 1 {
		t.Errorf("%d alerts, want 1", n)
	}
}

func TestPushProtection_SSH_ClosedAlertPassesOnRetry(t *testing.T) {
	e := newSSHPushEnv(t, true)
	c := testutil.NewPushClient(t, e.repo)
	bad := c.Commit(e.base, keyFile(), "add key")
	if got := e.push(t, c, create("topic", bad))[plumbing.NewBranchReferenceName("topic")]; got != e.wantRefusal("1") {
		t.Fatalf("first push status = %q", got)
	}
	err := e.svcs.SecretAlert.Allow(context.Background(), e.alerts(t)[0].ID, e.userID, model.SecretResolutionUsedInTests)
	if err != nil {
		t.Fatalf("allow: %v", err)
	}

	got := e.push(t, c, create("topic", bad))[plumbing.NewBranchReferenceName("topic")]

	if got != "ok" {
		t.Errorf("retry status = %q, want ok", got)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		locs, _ := e.svcs.SecretAlert.Locations(context.Background(), e.alerts(t)[0].ID)
		if len(locs) == 1 && locs[0].ReachedRef {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("location not marked reached: %+v", locs)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestPushProtection_SSH_AnnotatedTagAndOffSwitch(t *testing.T) {
	e := newSSHPushEnv(t, true)
	c := testutil.NewPushClient(t, e.repo)
	tag := c.Tag(c.Commit(e.base, keyFile(), "add key"), "v1")
	tagRef := plumbing.NewTagReferenceName("v1")

	refs := e.push(t, c, &packp.Command{Name: tagRef, New: tag})

	if got, want := refs[tagRef], e.wantRefusal("1"); got != want {
		t.Errorf("tag status = %q, want %q", got, want)
	}
	if strings.Contains(refs[tagRef], "internal error") {
		t.Errorf("tag status leaks an internal error: %q", refs[tagRef])
	}
}

func TestPushProtection_SSH_ProtectionOff_PassesAndAlerts(t *testing.T) {
	e := newSSHPushEnv(t, false)
	c := testutil.NewPushClient(t, e.repo)
	bad := c.Commit(e.base, keyFile(), "add key")

	refs := e.push(t, c, create("topic", bad))

	if got := refs[plumbing.NewBranchReferenceName("topic")]; got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(e.alerts(t)) != 1 {
		if time.Now().After(deadline) {
			t.Fatal("no alert after the push")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
