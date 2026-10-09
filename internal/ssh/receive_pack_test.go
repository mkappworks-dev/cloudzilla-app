package ssh

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
	"github.com/mkappworks-dev/cloudzilla-app/internal/metrics"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	gossh "golang.org/x/crypto/ssh"
)

var (
	mainRef    = plumbing.NewBranchReferenceName("main")
	featureRef = plumbing.NewBranchReferenceName("feature")
)

// pushRepo is a repo served over SSH whose bare git repo has main and feature,
// branched from one commit. mainPushed and featurePushed are commits on top of
// each tip that no branch points at yet.
type pushRepo struct {
	db                        *sql.DB
	id                        int64
	addr, path                string
	key                       gossh.Signer
	reposRoot, gitDir         string
	git                       *gogit.Repository
	mainTip, mainPushed       plumbing.Hash
	featureTip, featurePushed plumbing.Hash
}

func seedPushRepo(t *testing.T) pushRepo {
	t.Helper()
	return seedPushRepoWithQuota(t, config.QuotaConfig{})
}

func seedPushRepoWithQuota(t *testing.T, quota config.QuotaConfig) pushRepo {
	t.Helper()
	db := testutil.OpenTestDB(t)
	r := pushRepo{db: db, reposRoot: t.TempDir(), key: newKey(t)}
	cfg := &config.Config{Git: config.GitConfig{ReposRoot: r.reposRoot, SSHHostKey: filepath.Join(t.TempDir(), "host_key")}, Quota: quota}
	svcs := service.New(store.New(db), cfg)
	r.addr = serve(t, New(cfg.Git, svcs))

	sfx := testutil.UniqueSuffix(t)
	owner, name := "testuser_"+sfx, "testrepo_"+sfx
	ownerID := testutil.SeedUser(t, db, sfx)
	r.id = testutil.SeedRepo(t, db, ownerID, owner, sfx)
	r.path = "/" + owner + "/" + name + ".git"
	if _, err := svcs.SSHKey.AddKey(context.Background(), ownerID, "k", string(gossh.MarshalAuthorizedKey(r.key.PublicKey()))); err != nil {
		t.Fatalf("add key: %v", err)
	}

	r.gitDir = filepath.Join(r.reposRoot, owner, name+".git")
	git, err := gogit.PlainInit(r.gitDir, true)
	if err != nil {
		t.Fatalf("init bare repo: %v", err)
	}
	r.git = git
	base := testutil.WriteCommit(t, git.Storer, "base")
	r.mainTip = testutil.WriteCommit(t, git.Storer, "main", base)
	r.featureTip = testutil.WriteCommit(t, git.Storer, "feature", base)
	r.mainPushed = testutil.WriteCommit(t, git.Storer, "main pushed", r.mainTip)
	r.featurePushed = testutil.WriteCommit(t, git.Storer, "feature pushed", r.featureTip)
	for ref, h := range map[plumbing.ReferenceName]plumbing.Hash{mainRef: r.mainTip, featureRef: r.featureTip} {
		if err := git.Storer.SetReference(plumbing.NewHashReference(ref, h)); err != nil {
			t.Fatalf("set %s: %v", ref, err)
		}
	}
	return r
}

// receivePack pushes cmds as the repo owner, with a pack of the commits they
// point at.
func (r pushRepo) receivePack(t *testing.T, cmds ...*packp.Command) sshResult {
	t.Helper()
	var tips []plumbing.Hash
	for _, cmd := range cmds {
		if r.git.Storer.HasEncodedObject(cmd.New) == nil {
			tips = append(tips, cmd.New)
		}
	}
	var pack bytes.Buffer
	if _, err := packfile.NewEncoder(&pack, r.git.Storer, false).Encode(tips, 0); err != nil {
		t.Fatalf("encode pack: %v", err)
	}
	return r.receivePackOf(t, pack.Bytes(), cmds...)
}

// receivePackOf pushes cmds and pack as the repo owner, asking for
// report-status. Its stdout starts after the ref advertisement.
func (r pushRepo) receivePackOf(t *testing.T, pack []byte, cmds ...*packp.Command) sshResult {
	t.Helper()
	upd := packp.NewReferenceUpdateRequest()
	_ = upd.Capabilities.Set(capability.ReportStatus)
	upd.Commands = cmds
	upd.Packfile = io.NopCloser(bytes.NewReader(pack))

	client := dial(t, r.addr, r.key)
	defer func() { _ = client.Close() }()
	sess, err := client.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	in, err := sess.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	out, err := sess.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	var stdout, stderr bytes.Buffer
	sess.Stderr = &stderr
	command := "git-receive-pack '" + r.path + "'"
	if err := sess.Start(command); err != nil {
		t.Fatalf("start %q: %v", command, err)
	}

	// Each step blocks on the server, so a server that stops responding fails
	// the test instead of stalling it.
	step := func(what string, do func() error) {
		t.Helper()
		hung := time.AfterFunc(sessionTimeout, func() { _ = client.Close() })
		err := do()
		if !hung.Stop() {
			t.Fatalf("%s: no response within %v", what, sessionTimeout)
		}
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	step("read ref advertisement", func() error { return packp.NewAdvRefs().Decode(out) })
	step("send update request", func() error { return upd.Encode(in) })
	// git keeps stdin open until it has read the status, so a server that waits
	// for EOF first never reports. Whether stdout holds a status is for the
	// test to judge.
	step("read report status", func() error {
		_ = packp.NewReportStatus().Decode(io.TeeReader(out, &stdout))
		return nil
	})
	_ = in.Close()
	step("read rest of stdout", func() error {
		_, err := io.Copy(&stdout, out)
		return err
	})
	step("wait for exit", func() error {
		err = sess.Wait()
		return nil
	})
	return sshResult{stdout: stdout.String(), stderr: stderr.String(), status: exitStatus(t, command, err)}
}

// reportedRefs is the status of each ref in stdout, which must be only a
// report status.
func reportedRefs(t *testing.T, stdout string) map[plumbing.ReferenceName]string {
	t.Helper()
	r := strings.NewReader(stdout)
	status := packp.NewReportStatus()
	if err := status.Decode(r); err != nil {
		t.Fatalf("decode report status from %q: %v", stdout, err)
	}
	if r.Len() > 0 {
		t.Errorf("stdout continues after the report status: %q", stdout)
	}
	refs := make(map[plumbing.ReferenceName]string)
	for _, cs := range status.CommandStatuses {
		refs[cs.ReferenceName] = cs.Status
	}
	return refs
}

func assertRef(t *testing.T, r pushRepo, name plumbing.ReferenceName, want plumbing.Hash) {
	t.Helper()
	ref, err := r.git.Reference(name, false)
	if err != nil {
		t.Fatalf("resolve %s: %v", name, err)
	}
	if ref.Hash() != want {
		t.Errorf("%s = %s, want %s", name, ref.Hash(), want)
	}
}

func TestReceivePack_FastForward_Applies(t *testing.T) {
	r := seedPushRepo(t)
	ops := metrics.GitOperations.WithLabelValues("ssh", "receive-pack", "ok")
	bytesIn := metrics.GitBytes.WithLabelValues("ssh", "receive-pack")
	opsBefore, bytesBefore := promtestutil.ToFloat64(ops), promtestutil.ToFloat64(bytesIn)

	got := r.receivePack(t, &packp.Command{Name: mainRef, Old: r.mainTip, New: r.mainPushed})

	if d := promtestutil.ToFloat64(ops) - opsBefore; d != 1 {
		t.Errorf("git_operations_total{ssh,receive-pack,ok} delta = %v, want 1", d)
	}
	if d := promtestutil.ToFloat64(bytesIn) - bytesBefore; d <= 0 {
		t.Errorf("git_bytes_total{ssh,receive-pack} delta = %v, want > 0", d)
	}

	if got.stderr != "" || got.status != 0 {
		t.Errorf("got stderr %q, exit %d; want no stderr, exit 0", got.stderr, got.status)
	}
	if status := reportedRefs(t, got.stdout)[mainRef]; status != "ok" {
		t.Errorf("main status = %q, want ok", status)
	}
	assertRef(t, r, mainRef, r.mainPushed)
}

// go-git reports the unpack error's text, and an error storing the pushed
// objects names paths on the server.
func TestReceivePack_ObjectWriteFails_ErrorHidesServerPath(t *testing.T) {
	r := seedPushRepo(t)
	// go-git stages every received object in objects/pack before moving it
	// into place, even one the repo already has.
	packDir := filepath.Join(r.gitDir, "objects", "pack")
	if err := os.Chmod(packDir, 0o555); err != nil {
		t.Fatalf("make objects/pack read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(packDir, 0o755) })

	got := r.receivePack(t, &packp.Command{Name: mainRef, Old: r.mainTip, New: r.mainPushed})

	if got.status != 1 || !strings.Contains(got.stderr, gittransport.ErrUnpackFailed.Error()) {
		t.Errorf("got stderr %q, exit %d; want %q, exit 1", got.stderr, got.status, gittransport.ErrUnpackFailed.Error())
	}
	if strings.Contains(got.stdout+got.stderr, r.reposRoot) {
		t.Errorf("output names a server path: stdout %q, stderr %q", got.stdout, got.stderr)
	}
	assertRef(t, r, mainRef, r.mainTip)
}

// git sends no pack when every command is a delete, and keeps stdin open until
// it reads the status.
func TestReceivePack_DeleteOnly_ReportsAndExits(t *testing.T) {
	r := seedPushRepo(t)

	got := r.receivePackOf(t, nil, &packp.Command{Name: featureRef, Old: r.featureTip, New: plumbing.ZeroHash})

	if got.stderr != "" || got.status != 0 {
		t.Errorf("got stderr %q, exit %d; want no stderr, exit 0", got.stderr, got.status)
	}
	if status := reportedRefs(t, got.stdout)[featureRef]; status != "ok" {
		t.Errorf("feature status = %q, want ok", status)
	}
	if _, err := r.git.Reference(featureRef, false); err != plumbing.ErrReferenceNotFound {
		t.Errorf("feature lookup err = %v, want %v", err, plumbing.ErrReferenceNotFound)
	}
}

func TestReceivePack_ForcePushToProtectedBranch_Refused(t *testing.T) {
	r := seedPushRepo(t)
	testutil.Exec(t, r.db, `INSERT INTO branch_protections (repo_id, pattern, block_force_push) VALUES ($1, 'main', true)`, r.id)

	got := r.receivePack(t, &packp.Command{Name: mainRef, Old: r.mainTip, New: r.featurePushed})

	if got.stderr != "" || got.status != 0 {
		t.Errorf("got stderr %q, exit %d; want no stderr, exit 0", got.stderr, got.status)
	}
	if status := reportedRefs(t, got.stdout)[mainRef]; status != service.ErrForcePushBlocked.Error() {
		t.Errorf("main status = %q, want %q", status, service.ErrForcePushBlocked.Error())
	}
	assertRef(t, r, mainRef, r.mainTip)
}

func TestUploadPack_CountsOperationAndBytes(t *testing.T) {
	r := seedPushRepo(t)
	ops := metrics.GitOperations.WithLabelValues("ssh", "upload-pack", "ok")
	sent := metrics.GitBytes.WithLabelValues("ssh", "upload-pack")
	opsBefore, sentBefore := promtestutil.ToFloat64(ops), promtestutil.ToFloat64(sent)

	req := packp.NewUploadPackRequest()
	req.Wants = []plumbing.Hash{r.mainTip}
	var in bytes.Buffer
	if err := req.UploadRequest.Encode(&in); err != nil {
		t.Fatalf("encode request: %v", err)
	}
	got := runSSH(t, r.addr, r.key, "git-upload-pack '"+r.path+"'", in.String())

	if got.status != 0 {
		t.Fatalf("exit %d, stderr %q", got.status, got.stderr)
	}
	if d := promtestutil.ToFloat64(ops) - opsBefore; d != 1 {
		t.Errorf("git_operations_total{ssh,upload-pack,ok} delta = %v, want 1", d)
	}
	if d := promtestutil.ToFloat64(sent) - sentBefore; d <= 0 {
		t.Errorf("git_bytes_total{ssh,upload-pack} delta = %v, want > 0", d)
	}
}
