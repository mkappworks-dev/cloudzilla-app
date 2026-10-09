package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	gogitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/plumbing/transport/server"

	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// bareRepoWithCommit makes a bare repo whose main branch holds one commit.
func bareRepoWithCommit(t *testing.T, dir string) (*gogit.Repository, plumbing.Hash) {
	t.Helper()
	repo := testutil.InitBareRepo(t, dir)
	h := testutil.WriteCommit(t, repo.Storer, "first")
	for _, ref := range []*plumbing.Reference{
		plumbing.NewHashReference("refs/heads/main", h),
		plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/main"),
	} {
		if err := repo.Storer.SetReference(ref); err != nil {
			t.Fatal(err)
		}
	}
	return repo, h
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyRepos(t *testing.T, root string, afterRefs func(string)) (*bytes.Buffer, Section) {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	c := &repoCopier{tw: tw, root: root, prefix: reposDir, afterRefs: afterRefs}
	if err := c.run(context.Background()); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, c.stats
}

func entryNames(t *testing.T, buf *bytes.Buffer) []string {
	t.Helper()
	var names []string
	tr := tar.NewReader(bytes.NewReader(buf.Bytes()))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return names
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
	}
}

func TestRepoCopier_OrdersRefsBeforeObjectsAndSkipsScratch(t *testing.T) {
	root := t.TempDir()
	bareRepoWithCommit(t, filepath.Join(root, "alice", "app.git"))
	bareRepoWithCommit(t, filepath.Join(root, "alice", "old.git.deleted.1700000000"))
	writeFile(t, filepath.Join(root, ".import-tmp", "clone", "junk"), "x")
	writeFile(t, filepath.Join(root, ".readyz-123"), "x")
	writeFile(t, filepath.Join(root, "alice", "app.git", "refs", "heads", "busy.lock"), "x")
	writeFile(t, filepath.Join(root, "alice", "app.git", "objects", "pack", "tmp_pack_1"), "x")

	buf, stats := copyRepos(t, root, nil)
	names := entryNames(t, buf)

	for _, n := range names {
		if strings.Contains(n, ".import-tmp") || strings.Contains(n, ".readyz-") || strings.HasSuffix(n, ".lock") || strings.Contains(n, "tmp_pack") {
			t.Errorf("scratch entry copied: %s", n)
		}
	}
	for _, want := range []string{"git-repos/alice/app.git/HEAD", "git-repos/alice/old.git.deleted.1700000000/HEAD", "git-repos/alice/app.git/refs/heads/main"} {
		if !slices.Contains(names, want) {
			t.Errorf("missing %s", want)
		}
	}
	if stats.Files == 0 || stats.Bytes == 0 {
		t.Errorf("stats = %+v, want files and bytes counted", stats)
	}

	lastRef, firstObject := -1, len(names)
	for i, n := range names {
		if !strings.HasPrefix(n, "git-repos/alice/app.git/") {
			continue
		}
		rel := strings.TrimPrefix(n, "git-repos/alice/app.git/")
		switch {
		case rel == "HEAD" || rel == "config" || rel == "packed-refs" || strings.HasPrefix(rel, "refs"):
			lastRef = i
		case strings.HasPrefix(rel, "objects") && i < firstObject:
			firstObject = i
		}
	}
	if lastRef == -1 || lastRef > firstObject {
		t.Errorf("last ref entry at %d, first object entry at %d: refs must come first", lastRef, firstObject)
	}
	packAt, looseAt := -1, -1
	for i, n := range names {
		if strings.HasPrefix(n, "git-repos/alice/app.git/objects/pack/") {
			packAt = i
		} else if len(n) > len("git-repos/alice/app.git/objects/xx/") && strings.HasPrefix(n, "git-repos/alice/app.git/objects/") && !strings.Contains(n, "/pack") && !strings.Contains(n, "/info") {
			looseAt = i
		}
	}
	if looseAt != -1 && packAt != -1 && packAt < looseAt {
		t.Errorf("pack entry at %d precedes loose object at %d", packAt, looseAt)
	}
}

// extractAll replays a tar of repos into dir through the production checks.
func extractAll(t *testing.T, buf *bytes.Buffer, dir string) {
	t.Helper()
	x := extractor{root: dir}
	tr := tar.NewReader(bytes.NewReader(buf.Bytes()))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		_, rel, err := checkEntry(h.Name, h.Typeflag)
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeDir {
			err = x.dir(rel, h.FileInfo().Mode())
		} else {
			_, err = x.file(rel, tr, h.FileInfo().Mode(), h.ModTime)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestRepoCopier_PushDuringCopyStillRestoresEveryRef(t *testing.T) {
	root := t.TempDir()
	serverDir := filepath.Join(root, "alice", "app.git")
	server_, first := bareRepoWithCommit(t, serverDir)

	// A client that already has the first commit and will push a second.
	clientRepo, err := gogit.PlainClone(t.TempDir(), false, &gogit.CloneOptions{URL: serverDir})
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	wt, _ := clientRepo.Worktree()
	second, err := wt.Commit("second", &gogit.CommitOptions{
		AllowEmptyCommits: true,
		Author:            &object.Signature{Name: "T", Email: "t@example.com", When: time.Unix(1, 0)},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	const scheme = "backuptest"
	client.InstallProtocol(scheme, server.NewServer(server.MapLoader{
		scheme + "://" + serverDir: gittransport.WrapForReceive(server_.Storer),
	}))
	t.Cleanup(func() { client.InstallProtocol(scheme, nil) })
	if _, err := clientRepo.CreateRemote(&gogitconfig.RemoteConfig{Name: "wrapped", URLs: []string{scheme + "://" + serverDir}}); err != nil {
		t.Fatal(err)
	}

	pushed := false
	buf, _ := copyRepos(t, root, func(gitDir string) {
		if gitDir != serverDir {
			return
		}
		if err := clientRepo.Push(&gogit.PushOptions{RemoteName: "wrapped"}); err != nil {
			t.Errorf("push during copy: %v", err)
			return
		}
		pushed = true
	})
	if !pushed {
		t.Fatal("the push never ran")
	}

	restored := t.TempDir()
	extractAll(t, buf, restored)
	repo, err := gogit.PlainOpen(filepath.Join(restored, "alice", "app.git"))
	if err != nil {
		t.Fatal(err)
	}
	main, err := repo.Reference("refs/heads/main", true)
	if err != nil {
		t.Fatal(err)
	}
	if main.Hash() != first {
		t.Fatalf("copy has main at %s; the refs should predate the push (%s)", main.Hash(), first)
	}
	refs, _ := repo.References()
	_ = refs.ForEach(func(r *plumbing.Reference) error {
		if r.Type() != plumbing.HashReference {
			return nil
		}
		c, err := repo.CommitObject(r.Hash())
		if err != nil {
			t.Errorf("%s -> %s does not resolve: %v", r.Name(), r.Hash(), err)
			return nil
		}
		if _, err := repo.Log(&gogit.LogOptions{From: c.Hash}); err != nil {
			t.Errorf("walk %s: %v", r.Name(), err)
		}
		return nil
	})
	if _, err := repo.CommitObject(second); err != nil {
		t.Errorf("objects were copied before the push finished, so the new commit %s is missing: %v", second, err)
	}
}

func TestExtractor_RefusesNonEmptyRoot(t *testing.T) {
	dir := t.TempDir()
	if err := rootEmpty(dir); err != nil {
		t.Fatalf("empty dir: %v", err)
	}
	if err := rootEmpty(filepath.Join(dir, "missing")); err != nil {
		t.Fatalf("missing dir: %v", err)
	}
	writeFile(t, filepath.Join(dir, "x"), "x")
	if err := rootEmpty(dir); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("err = %v, want a not-empty refusal", err)
	}
}
