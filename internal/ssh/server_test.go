package ssh

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"
	gogit "github.com/go-git/go-git/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
	gossh "golang.org/x/crypto/ssh"
)

const sessionTimeout = 10 * time.Second

type sshResult struct {
	stdout, stderr string
	status         int
}

func serve(t *testing.T, s *Server) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = s.srv.Serve(ln) }()
	t.Cleanup(func() { _ = s.srv.Close() })
	return ln.Addr().String()
}

func newKey(t *testing.T) gossh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	return signer
}

// runSSH runs command the way git's ssh transport does: it writes stdin and
// keeps it open until the server ends the session, and keeps stdout and stderr
// apart. An empty command requests a shell.
func runSSH(t *testing.T, addr string, key gossh.Signer, command, stdin string) sshResult {
	t.Helper()
	client := dial(t, addr, key)
	defer func() { _ = client.Close() }()
	hung := time.AfterFunc(sessionTimeout, func() { _ = client.Close() })
	sess, err := client.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	var stdout, stderr bytes.Buffer
	sess.Stdout = &stdout
	sess.Stderr = &stderr
	in, err := sess.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	if command == "" {
		err = sess.Shell()
	} else {
		err = sess.Start(command)
	}
	if err != nil {
		t.Fatalf("start %q: %v", command, err)
	}
	// The server stops reading early when it refuses the request.
	_, _ = io.WriteString(in, stdin)
	err = sess.Wait()
	if !hung.Stop() {
		t.Fatalf("%q: the server didn't end the session within %v", command, sessionTimeout)
	}
	return sshResult{stdout: stdout.String(), stderr: stderr.String(), status: exitStatus(t, command, err)}
}

func dial(t *testing.T, addr string, key gossh.Signer) *gossh.Client {
	t.Helper()
	client, err := gossh.Dial("tcp", addr, &gossh.ClientConfig{
		User:            "git",
		Auth:            []gossh.AuthMethod{gossh.PublicKeys(key)},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         sessionTimeout,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return client
}

// exitStatus is the status command exited with, given what Wait returned.
func exitStatus(t *testing.T, command string, err error) int {
	t.Helper()
	var exitErr *gossh.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exitErr):
		return exitErr.ExitStatus()
	default:
		t.Fatalf("run %q: %v", command, err)
		return 0
	}
}

func TestSessionHandler_RequestErrorsGoToStderr(t *testing.T) {
	s := New(config.GitConfig{SSHHostKey: filepath.Join(t.TempDir(), "host_key")}, nil)
	// publicKeyHandler needs the database; these errors come before any lookup.
	s.srv.PublicKeyHandler = func(ssh.Context, ssh.PublicKey) bool { return true }
	addr := serve(t, s)
	key := newKey(t)

	for _, tc := range []struct{ name, command, want string }{
		{"no command", "", "no git command provided\n"},
		{"unsupported command", "ls", "unsupported git command: ls\n"},
		{"no repository path", "git-upload-pack", "git command requires repository path\n"},
		{"no identity", "git-upload-pack '/owner/repo.git'", "authentication required\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runSSH(t, addr, key, tc.command, "")
			want := sshResult{stderr: tc.want, status: 1}
			if got != want {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}

func TestSessionHandler_RepoErrorsGoToStderr(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	reposRoot := t.TempDir()
	cfg := &config.Config{Git: config.GitConfig{ReposRoot: reposRoot, SSHHostKey: filepath.Join(t.TempDir(), "host_key")}}
	svcs := service.New(store.New(db), cfg)
	addr := serve(t, New(cfg.Git, svcs))

	sfx := testutil.UniqueSuffix(t)
	me, other := "testuser_me_"+sfx, "testuser_other_"+sfx
	meID := testutil.SeedUser(t, db, "me_"+sfx)
	otherID := testutil.SeedUser(t, db, "other_"+sfx)
	mine, archived, theirs := "testrepo_mine_"+sfx, "testrepo_archived_"+sfx, "testrepo_theirs_"+sfx
	mineID := testutil.SeedRepo(t, db, meID, me, "mine_"+sfx)
	archivedID := testutil.SeedRepo(t, db, meID, me, "archived_"+sfx)
	theirsID := testutil.SeedRepo(t, db, otherID, other, "theirs_"+sfx)
	testutil.Exec(t, db, `UPDATE repositories SET is_archived = true WHERE id = $1`, archivedID)
	testutil.Exec(t, db, `UPDATE repositories SET private = true WHERE id = $1`, theirsID)
	if _, err := gogit.PlainInit(filepath.Join(reposRoot, me, mine+".git"), true); err != nil {
		t.Fatalf("init %s: %v", mine, err)
	}

	userKey, deployKey := newKey(t), newKey(t)
	if _, err := svcs.SSHKey.AddKey(ctx, meID, "k", string(gossh.MarshalAuthorizedKey(userKey.PublicKey()))); err != nil {
		t.Fatalf("add user key: %v", err)
	}
	if _, err := svcs.DeployKey.Add(ctx, mineID, "d", string(gossh.MarshalAuthorizedKey(deployKey.PublicKey())), true); err != nil {
		t.Fatalf("add deploy key: %v", err)
	}

	for _, tc := range []struct {
		name    string
		key     gossh.Signer
		command string
		want    string
	}{
		{"invalid path", userKey, "git-upload-pack '/" + mine + ".git'", "invalid repository path format\n"},
		{"repository not found", userKey, "git-upload-pack '/" + me + "/missing.git'", "repository not found\n"},
		{"no read access", userKey, "git-upload-pack '/" + other + "/" + theirs + ".git'", "access denied\n"},
		{"no write access", userKey, "git-receive-pack '/" + other + "/" + theirs + ".git'", "access denied\n"},
		{"push to archived repo", userKey, "git-receive-pack '/" + me + "/" + archived + ".git'", "Repository is archived and read-only.\n"},
		{"no repository directory", userKey, "git-upload-pack '/" + me + "/" + archived + ".git'", "failed to open repository\n"},
		{"deploy key for another repo", deployKey, "git-upload-pack '/" + me + "/" + archived + ".git'", "deploy key not authorized for this repository\n"},
		{"read-only deploy key push", deployKey, "git-receive-pack '/" + me + "/" + mine + ".git'", "deploy key is read-only\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runSSH(t, addr, tc.key, tc.command, "")
			want := sshResult{stderr: tc.want, status: 1}
			if got != want {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}

	t.Run("error after the ref advertisement", func(t *testing.T) {
		got := runSSH(t, addr, userKey, "git-upload-pack '/"+me+"/"+mine+".git'", "000ahello\n")
		if !strings.HasPrefix(got.stderr, "error: decode upload-pack request: ") || got.status != 1 {
			t.Errorf("got stderr %q, exit %d; want the decode error on stderr, exit 1", got.stderr, got.status)
		}
		if strings.Contains(got.stdout, "error") {
			t.Errorf("stdout carries the error: %q", got.stdout)
		}
	})

	for _, svc := range []string{"git-upload-pack", "git-receive-pack"} {
		t.Run(svc+" flush-only request", func(t *testing.T) {
			got := runSSH(t, addr, userKey, svc+" '/"+me+"/"+mine+".git'", "0000")
			if got.stderr != "" || got.status != 0 {
				t.Errorf("got stderr %q, exit %d; want no stderr, exit 0", got.stderr, got.status)
			}
		})
	}
}
