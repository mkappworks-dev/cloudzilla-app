package service_test

// Integration tests for ReleaseService. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	gitobj "github.com/go-git/go-git/v5/plumbing/object"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// seedReleaseRepo creates a bare repo at <root>/<owner>/<name>.git holding one commit
// on a "main" branch, tagged with each given tag, then returns the ReposRoot.
// ReleaseService.Create reads the on-disk repo to find or create the release tag.
func seedReleaseRepo(t *testing.T, owner, name string, tags ...string) string {
	t.Helper()
	root := t.TempDir()

	bareDir := filepath.Join(root, owner, name+".git")
	if err := os.MkdirAll(filepath.Dir(bareDir), 0o755); err != nil {
		t.Fatalf("mkdir owner: %v", err)
	}
	if _, err := gogit.PlainInit(bareDir, true); err != nil {
		t.Fatalf("plain init bare: %v", err)
	}

	workDir := t.TempDir()
	work, err := gogit.PlainInit(workDir, false)
	if err != nil {
		t.Fatalf("plain init work: %v", err)
	}
	wt, err := work.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "f.txt"), []byte("seed"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if _, err := wt.Add("f.txt"); err != nil {
		t.Fatalf("add: %v", err)
	}
	sig := &gitobj.Signature{Name: "Tester", Email: "tester@test.invalid", When: time.Now().UTC()}
	commit, err := wt.Commit("seed commit", &gogit.CommitOptions{Author: sig, Committer: sig})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	for _, tag := range tags {
		if _, err := work.CreateTag(tag, commit, nil); err != nil {
			t.Fatalf("create tag %s: %v", tag, err)
		}
	}

	if _, err := work.CreateRemote(&gitconfig.RemoteConfig{
		Name: "bare",
		URLs: []string{bareDir},
	}); err != nil {
		t.Fatalf("create remote: %v", err)
	}
	head, err := work.Head()
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	refSpecs := []gitconfig.RefSpec{gitconfig.RefSpec(head.Name().String() + ":refs/heads/main")}
	if len(tags) > 0 {
		refSpecs = append(refSpecs, "refs/tags/*:refs/tags/*")
	}
	if err := work.Push(&gogit.PushOptions{RemoteName: "bare", RefSpecs: refSpecs}); err != nil {
		t.Fatalf("push: %v", err)
	}
	return root
}

// newReleaseSvc builds a ReleaseService backed by the test database and an on-disk git
// repo seeded with the given tags. Returns the service, ownerName, repoName, and owner ID.
func newReleaseSvc(t *testing.T, tags ...string) (*service.ReleaseService, string, string, int64) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoName := "testrepo_" + suffix
	testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	reposRoot := seedReleaseRepo(t, ownerName, repoName, tags...)
	svc := service.NewReleaseService(
		store.NewReleaseStore(db),
		store.NewRepoStore(db),
		service.NewCodeService(config.GitConfig{ReposRoot: reposRoot}),
	)
	return svc, ownerName, repoName, ownerID
}

// TestReleaseService_Create_AssignsID verifies that Create inserts a release and
// returns it with a non-zero database ID.
func TestReleaseService_Create_AssignsID(t *testing.T) {
	svc, owner, repo, authorID := newReleaseSvc(t, "v1.0.0")

	r, err := svc.Create(context.Background(), owner, repo, "v1.0.0", "main", "Release 1.0", "First release", false, false, authorID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if r.ID == 0 {
		t.Error("Create must return a release with non-zero ID")
	}
	if r.TagName != "v1.0.0" {
		t.Errorf("want tag %q, got %q", "v1.0.0", r.TagName)
	}
}

// TestReleaseService_ListByRepo_ReturnsRelease verifies that ListByRepo returns the
// release we just created.
func TestReleaseService_ListByRepo_ReturnsRelease(t *testing.T) {
	svc, owner, repo, authorID := newReleaseSvc(t, "v2.0.0")

	if _, err := svc.Create(context.Background(), owner, repo, "v2.0.0", "main", "Release 2.0", "", false, false, authorID); err != nil {
		t.Fatalf("Create: %v", err)
	}

	releases, err := svc.ListByRepo(context.Background(), owner, repo)
	if err != nil {
		t.Fatalf("ListByRepo: %v", err)
	}
	if len(releases) == 0 {
		t.Error("ListByRepo must return at least the release we created")
	}
}

// TestReleaseService_GetByTag_ReturnsCorrectRelease verifies that GetByTag finds the
// release by its tag name.
func TestReleaseService_GetByTag_ReturnsCorrectRelease(t *testing.T) {
	svc, owner, repo, authorID := newReleaseSvc(t, "v3.0.0")

	r, err := svc.Create(context.Background(), owner, repo, "v3.0.0", "main", "Release 3.0", "", false, false, authorID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	found, err := svc.GetByTag(context.Background(), owner, repo, "v3.0.0")
	if err != nil {
		t.Fatalf("GetByTag: %v", err)
	}
	if found.ID != r.ID {
		t.Errorf("want release ID %d, got %d", r.ID, found.ID)
	}
}

// TestReleaseService_Delete_RemovesRelease verifies that Delete removes the release so
// GetByTag returns an error afterward.
func TestReleaseService_Delete_RemovesRelease(t *testing.T) {
	svc, owner, repo, authorID := newReleaseSvc(t, "v4.0.0")

	r, err := svc.Create(context.Background(), owner, repo, "v4.0.0", "main", "Release 4.0", "", false, false, authorID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.Delete(context.Background(), owner, repo, r.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = svc.GetByTag(context.Background(), owner, repo, "v4.0.0")
	if err == nil {
		t.Error("GetByTag must return an error after the release is deleted")
	}
}

// TestReleaseService_Create_Prerelease verifies that a release created with
// isPrerelease=true stores the flag correctly.
func TestReleaseService_Create_Prerelease(t *testing.T) {
	svc, owner, repo, authorID := newReleaseSvc(t, "v1.0.0-rc1")

	r, err := svc.Create(context.Background(), owner, repo, "v1.0.0-rc1", "main", "Release Candidate", "", true, false, authorID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !r.IsPrerelease {
		t.Error("Create with isPrerelease=true must set IsPrerelease=true")
	}
}

// TestReleaseService_Create_CreatesTagOnTargetBranch verifies that Create tags the
// target branch's tip when the release tag does not yet exist.
func TestReleaseService_Create_CreatesTagOnTargetBranch(t *testing.T) {
	svc, owner, repo, authorID := newReleaseSvc(t) // no pre-existing tags

	r, err := svc.Create(context.Background(), owner, repo, "v5.0.0", "main", "Release 5.0", "", false, false, authorID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if r.TagName != "v5.0.0" {
		t.Errorf("want tag %q, got %q", "v5.0.0", r.TagName)
	}

	found, err := svc.GetByTag(context.Background(), owner, repo, "v5.0.0")
	if err != nil {
		t.Fatalf("GetByTag after tag creation: %v", err)
	}
	if found.ID != r.ID {
		t.Errorf("want release ID %d, got %d", r.ID, found.ID)
	}
}

// TestReleaseService_Create_FailsWhenTargetBranchMissing verifies that Create fails
// when the tag is absent and the target branch cannot be resolved.
func TestReleaseService_Create_FailsWhenTargetBranchMissing(t *testing.T) {
	svc, owner, repo, authorID := newReleaseSvc(t) // no pre-existing tags

	_, err := svc.Create(context.Background(), owner, repo, "v6.0.0", "no-such-branch", "Release 6.0", "", false, false, authorID)
	if err == nil {
		t.Error("Create must fail when the target branch does not exist")
	}
}
