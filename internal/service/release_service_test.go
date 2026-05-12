package service_test

// Integration tests for ReleaseService. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// newReleaseSvc builds a ReleaseService backed by the test database and seeds an owner
// and repo. Returns the service, ownerName, and repoName.
func newReleaseSvc(t *testing.T) (*service.ReleaseService, string, string, int64) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoName := "testrepo_" + suffix
	testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	svc := service.NewReleaseService(
		store.NewReleaseStore(db),
		store.NewRepoStore(db),
		service.NewCodeService(config.GitConfig{}),
	)
	return svc, ownerName, repoName, ownerID
}

// TestReleaseService_Create_AssignsID verifies that Create inserts a release and
// returns it with a non-zero database ID.
func TestReleaseService_Create_AssignsID(t *testing.T) {
	svc, owner, repo, authorID := newReleaseSvc(t)

	r, err := svc.Create(context.Background(), owner, repo, "v1.0.0", "Release 1.0", "First release", false, false, authorID)
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
	svc, owner, repo, authorID := newReleaseSvc(t)

	if _, err := svc.Create(context.Background(), owner, repo, "v2.0.0", "Release 2.0", "", false, false, authorID); err != nil {
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
	svc, owner, repo, authorID := newReleaseSvc(t)

	r, err := svc.Create(context.Background(), owner, repo, "v3.0.0", "Release 3.0", "", false, false, authorID)
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
	svc, owner, repo, authorID := newReleaseSvc(t)

	r, err := svc.Create(context.Background(), owner, repo, "v4.0.0", "Release 4.0", "", false, false, authorID)
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
	svc, owner, repo, authorID := newReleaseSvc(t)

	r, err := svc.Create(context.Background(), owner, repo, "v1.0.0-rc1", "Release Candidate", "", true, false, authorID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !r.IsPrerelease {
		t.Error("Create with isPrerelease=true must set IsPrerelease=true")
	}
}
