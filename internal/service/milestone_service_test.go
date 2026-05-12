package service_test

// Integration tests for MilestoneService. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// newMilestoneSvc builds a MilestoneService backed by the test database and seeds an
// owner and repo. Returns the service, ownerName, and repoName.
func newMilestoneSvc(t *testing.T) (*service.MilestoneService, string, string) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoName := "testrepo_" + suffix
	testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	svc := service.NewMilestoneService(store.NewMilestoneStore(db), store.NewRepoStore(db))
	return svc, ownerName, repoName
}

// TestMilestoneService_Create_AssignsID verifies that Create inserts a milestone and
// returns it with a non-zero database ID and sequential number.
func TestMilestoneService_Create_AssignsID(t *testing.T) {
	svc, owner, repo := newMilestoneSvc(t)

	m, err := svc.Create(context.Background(), owner, repo, "v1.0", "First milestone", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if m.ID == 0 {
		t.Error("Create must return a milestone with non-zero ID")
	}
	if m.Number == 0 {
		t.Error("Create must assign a sequential number")
	}
	if m.Title != "v1.0" {
		t.Errorf("want title %q, got %q", "v1.0", m.Title)
	}
}

// TestMilestoneService_ListByRepo_ReturnsCreated verifies that ListByRepo returns the
// milestone we just created.
func TestMilestoneService_ListByRepo_ReturnsCreated(t *testing.T) {
	svc, owner, repo := newMilestoneSvc(t)

	if _, err := svc.Create(context.Background(), owner, repo, "Sprint 1", "", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}

	milestones, err := svc.ListByRepo(context.Background(), owner, repo)
	if err != nil {
		t.Fatalf("ListByRepo: %v", err)
	}
	if len(milestones) == 0 {
		t.Error("ListByRepo must return at least the milestone we created")
	}
}

// TestMilestoneService_Close_SetsClosedState verifies that Close transitions a milestone
// from open to closed and the state is persisted.
func TestMilestoneService_Close_SetsClosedState(t *testing.T) {
	svc, owner, repo := newMilestoneSvc(t)

	m, err := svc.Create(context.Background(), owner, repo, "Closeable", "", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	closed, err := svc.Close(context.Background(), owner, repo, m.Number)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if closed.State != "closed" {
		t.Errorf("want state closed, got %q", closed.State)
	}
}

// TestMilestoneService_Reopen_SetsOpenState verifies that Reopen transitions a closed
// milestone back to open.
func TestMilestoneService_Reopen_SetsOpenState(t *testing.T) {
	svc, owner, repo := newMilestoneSvc(t)

	m, err := svc.Create(context.Background(), owner, repo, "Reopenable", "", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Close(context.Background(), owner, repo, m.Number); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := svc.Reopen(context.Background(), owner, repo, m.Number)
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	if reopened.State != "open" {
		t.Errorf("want state open after Reopen, got %q", reopened.State)
	}
}

// TestMilestoneService_Delete_RemovesMilestone verifies that Delete removes the
// milestone so GetByNumber returns an error afterward.
func TestMilestoneService_Delete_RemovesMilestone(t *testing.T) {
	svc, owner, repo := newMilestoneSvc(t)

	m, err := svc.Create(context.Background(), owner, repo, "Deletable", "", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.Delete(context.Background(), owner, repo, m.Number); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = svc.GetByNumber(context.Background(), owner, repo, m.Number)
	if err == nil {
		t.Error("GetByNumber must return an error after the milestone is deleted")
	}
}
