package service_test

// Integration tests for PullService. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// newPullSvc builds a PullService backed by the test database, seeding an owner and repo.
// Returns the service, ownerID, ownerName, and repoName.
func newPullSvc(t *testing.T) (*service.PullService, int64, string, string) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoName := "testrepo_" + suffix
	testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	repoSvc := service.NewRepoService(store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db), nil, nil, nil, config.GitConfig{})
	svc := service.NewPullService(store.NewPullStore(db), store.NewRepoStore(db), repoSvc)
	return svc, ownerID, ownerName, repoName
}

// TestPullService_Create_AssignsID verifies that Create inserts a PR with a non-zero ID
// and the correct initial state (open).
func TestPullService_Create_AssignsID(t *testing.T) {
	svc, ownerID, ownerName, repoName := newPullSvc(t)

	pr, err := svc.Create(context.Background(), ownerName, repoName, ownerID,
		"Test PR", "body", "feature", "main", false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if pr.ID == 0 {
		t.Error("created PR must have non-zero ID")
	}
	if pr.State != model.PRStateOpen {
		t.Errorf("new PR must be open, got %q", pr.State)
	}
}

// TestPullService_Create_DraftPR verifies that a PR created with isDraft=true
// is stored as a draft.
func TestPullService_Create_DraftPR(t *testing.T) {
	svc, ownerID, ownerName, repoName := newPullSvc(t)

	pr, err := svc.Create(context.Background(), ownerName, repoName, ownerID,
		"Draft PR", "body", "draft-feature", "main", true)
	if err != nil {
		t.Fatalf("Create draft: %v", err)
	}
	if !pr.IsDraft {
		t.Error("PR created as draft must have IsDraft=true")
	}
}

// TestPullService_Create_UnknownRepo_Error verifies that Create returns an error
// when the repository does not exist.
func TestPullService_Create_UnknownRepo_Error(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoSvc := service.NewRepoService(store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db), nil, nil, nil, config.GitConfig{})
	svc := service.NewPullService(store.NewPullStore(db), store.NewRepoStore(db), repoSvc)

	_, err := svc.Create(context.Background(), "nobody", "nonexistent", ownerID,
		"title", "body", "feature", "main", false)
	if err == nil {
		t.Error("Create with unknown repo must return an error")
	}
}

// TestPullService_List_ReturnsCreatedPR verifies that List returns PRs that were
// previously created in the repository.
func TestPullService_List_ReturnsCreatedPR(t *testing.T) {
	svc, ownerID, ownerName, repoName := newPullSvc(t)

	_, err := svc.Create(context.Background(), ownerName, repoName, ownerID,
		"Listed PR", "body", "feature-list", "main", false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	prs, err := svc.List(context.Background(), ownerName, repoName)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(prs) == 0 {
		t.Error("List must return at least the PR we just created")
	}
}

// TestPullService_SetState_Close verifies that SetState transitions a PR from open to closed
// and the returned PR reflects the new state.
func TestPullService_SetState_Close(t *testing.T) {
	svc, ownerID, ownerName, repoName := newPullSvc(t)

	pr, err := svc.Create(context.Background(), ownerName, repoName, ownerID,
		"Closeable PR", "body", "feature-close", "main", false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	closed, err := svc.SetState(context.Background(), ownerName, repoName, pr.Number, model.PRStateClosed)
	if err != nil {
		t.Fatalf("SetState closed: %v", err)
	}
	if closed.State != model.PRStateClosed {
		t.Errorf("want closed state, got %q", closed.State)
	}
}

// TestPullService_SetState_MergedCannotBeUpdated verifies that attempting to change
// the state of an already-merged PR returns an error (merged state is final).
func TestPullService_SetState_MergedCannotBeUpdated(t *testing.T) {
	svc, ownerID, ownerName, repoName := newPullSvc(t)

	pr, err := svc.Create(context.Background(), ownerName, repoName, ownerID,
		"Merge-lock PR", "body", "feature-merge", "main", false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Mark as merged directly.
	if _, err := svc.SetState(context.Background(), ownerName, repoName, pr.Number, model.PRStateMerged); err != nil {
		t.Fatalf("SetState merged: %v", err)
	}

	// Attempting to re-open a merged PR must fail.
	_, err = svc.SetState(context.Background(), ownerName, repoName, pr.Number, model.PRStateOpen)
	if err == nil {
		t.Error("SetState must fail when PR is already merged")
	}
}
