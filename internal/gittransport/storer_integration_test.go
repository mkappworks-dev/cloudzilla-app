package gittransport_test

import (
	"os"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/plumbing/transport/server"

	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
)

// TestWrapForReceive_PushSucceedsThroughWrappedStorer is a transport smoke
// test: it drives a real go-git client push against an in-process server
// whose storer is wrapped by WrapForReceive, and verifies the push lands
// and links to pre-existing history.
//
// It does NOT reproduce the thin-pack bug. go-git's own client never emits
// a pack with an external REF_DELTA base, so this test passes with or
// without the wrapper. The load-bearing regression guard is the unit test
// TestWrapForReceive_HidesPackfileWriter; end-to-end thin-pack behavior is
// covered by manual verification with a native git client. This test only
// confirms the wrapped storer remains a valid drop-in for receive-pack.
func TestWrapForReceive_PushSucceedsThroughWrappedStorer(t *testing.T) {
	// 1. Bare server repo that will receive the push.
	serverDir := t.TempDir()
	serverRepo, err := gogit.PlainInit(serverDir, true)
	if err != nil {
		t.Fatalf("PlainInit server: %v", err)
	}

	// 2. Seed the server with one commit via a non-bare working dir.
	seedDir := t.TempDir()
	seedRepo, err := gogit.PlainInit(seedDir, false)
	if err != nil {
		t.Fatalf("PlainInit seed: %v", err)
	}
	commitFile(t, seedRepo, "README.md", "hello\n", "initial commit")
	if _, err := seedRepo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{serverDir},
	}); err != nil {
		t.Fatalf("CreateRemote seed: %v", err)
	}
	if err := seedRepo.Push(&gogit.PushOptions{}); err != nil {
		t.Fatalf("seed push: %v", err)
	}

	// 3. Clone server → client; client now has the initial commit.
	clientDir := t.TempDir()
	clientRepo, err := gogit.PlainClone(clientDir, false, &gogit.CloneOptions{
		URL: serverDir,
	})
	if err != nil {
		t.Fatalf("PlainClone client: %v", err)
	}

	// 4. Add a second commit in the client.
	commitFile(t, clientRepo, "README.md", "hello\nmodified\n", "modify readme")

	// 5. Install a custom protocol whose server-side storer is wrapped.
	//    The scheme is registered process-wide in an unsynchronized map
	//    (go-git v5 limitation): this test must not call t.Parallel(), or the
	//    map write races with concurrent InstallProtocol/NewClient calls. The
	//    cleanup below removes the scheme so it can't leak into other tests.
	const scheme = "wrappedtest"
	wrappedTransport := server.NewServer(server.MapLoader{
		scheme + "://" + serverDir: gittransport.WrapForReceive(serverRepo.Storer),
	})
	client.InstallProtocol(scheme, wrappedTransport)
	t.Cleanup(func() { client.InstallProtocol(scheme, nil) })

	if _, err := clientRepo.CreateRemote(&config.RemoteConfig{
		Name: "wrapped",
		URLs: []string{scheme + "://" + serverDir},
	}); err != nil {
		t.Fatalf("CreateRemote wrapped: %v", err)
	}

	// 6. Push through the wrapped transport.
	if err := clientRepo.Push(&gogit.PushOptions{RemoteName: "wrapped"}); err != nil {
		t.Fatalf("push through wrapped transport: %v", err)
	}

	// 7. Verify the server's HEAD advanced and links to pre-existing history.
	head, err := serverRepo.Head()
	if err != nil {
		t.Fatalf("serverRepo.Head: %v", err)
	}
	commit, err := serverRepo.CommitObject(head.Hash())
	if err != nil {
		t.Fatalf("serverRepo.CommitObject: %v", err)
	}
	if commit.Message != "modify readme" {
		t.Fatalf("expected server HEAD to be 'modify readme', got %q", commit.Message)
	}
	if commit.NumParents() != 1 {
		t.Fatalf("expected 1 parent, got %d", commit.NumParents())
	}
	// The parent came from the initial seed, so resolving it confirms the
	// pushed commit links cleanly onto pre-existing server objects.
	if _, err := commit.Parent(0); err != nil {
		t.Fatalf("parent commit not resolvable: %v", err)
	}
}

// commitFile writes content to repo's worktree at filename, stages it,
// and creates a commit. Returns the new commit's hash.
func commitFile(t *testing.T, repo *gogit.Repository, filename, content, message string) plumbing.Hash {
	t.Helper()

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	fs := wt.Filesystem
	f, err := fs.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if _, err := f.Write([]byte(content)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := wt.Add(filename); err != nil {
		t.Fatalf("Add: %v", err)
	}
	hash, err := wt.Commit(message, &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "test@example.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return hash
}
