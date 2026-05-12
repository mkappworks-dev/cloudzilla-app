package store_test

// Integration tests for PullStore. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// seedPullDeps seeds an owner user and repo, returning the pull store,
// repo ID, and owner ID for use in pull request tests.
func seedPullDeps(t *testing.T) (*store.PullStore, int64, int64) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	return store.NewPullStore(db), repoID, ownerID
}

// TestPullStore_Create_AssignsIDAndNumber verifies that Create inserts a pull request,
// assigns a non-zero database ID, and assigns a repo-scoped sequential number.
func TestPullStore_Create_AssignsIDAndNumber(t *testing.T) {
	ps, repoID, ownerID := seedPullDeps(t)

	pr := &model.PullRequest{
		RepoID:     repoID,
		AuthorID:   ownerID,
		Title:      "Test PR",
		Body:       "body",
		HeadBranch: "feature",
		BaseBranch: "main",
		State:      model.PRStateOpen,
	}
	if err := ps.Create(context.Background(), pr); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if pr.ID == 0 {
		t.Error("Create must assign a non-zero ID")
	}
	if pr.Number == 0 {
		t.Error("Create must assign a non-zero sequential number")
	}
}

// TestPullStore_GetByNumber_ReturnsCorrectPR verifies that GetByNumber finds a pull
// request by its repo-scoped number after creation.
func TestPullStore_GetByNumber_ReturnsCorrectPR(t *testing.T) {
	ps, repoID, ownerID := seedPullDeps(t)

	pr := &model.PullRequest{
		RepoID:     repoID,
		AuthorID:   ownerID,
		Title:      "Findable PR",
		HeadBranch: "feature",
		BaseBranch: "main",
		State:      model.PRStateOpen,
	}
	if err := ps.Create(context.Background(), pr); err != nil {
		t.Fatalf("Create: %v", err)
	}

	found, err := ps.GetByNumber(context.Background(), repoID, pr.Number)
	if err != nil {
		t.Fatalf("GetByNumber: %v", err)
	}
	if found.ID != pr.ID {
		t.Errorf("want PR ID %d, got %d", pr.ID, found.ID)
	}
	if found.Title != "Findable PR" {
		t.Errorf("want title %q, got %q", "Findable PR", found.Title)
	}
}

// TestPullStore_GetByNumber_Unknown_Error verifies that GetByNumber returns an error
// when no pull request with the given number exists in the repository.
func TestPullStore_GetByNumber_Unknown_Error(t *testing.T) {
	ps, repoID, _ := seedPullDeps(t)

	_, err := ps.GetByNumber(context.Background(), repoID, 9999)
	if err == nil {
		t.Error("GetByNumber must return an error for an unknown PR number")
	}
}

// TestPullStore_UpdateState_Closed verifies that UpdateState transitions an open pull
// request to the closed state and persists the change in the database.
func TestPullStore_UpdateState_Closed(t *testing.T) {
	ps, repoID, ownerID := seedPullDeps(t)

	pr := &model.PullRequest{
		RepoID:     repoID,
		AuthorID:   ownerID,
		Title:      "Closeable PR",
		HeadBranch: "feature",
		BaseBranch: "main",
		State:      model.PRStateOpen,
	}
	if err := ps.Create(context.Background(), pr); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := ps.UpdateState(context.Background(), pr.ID, model.PRStateClosed); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}

	found, err := ps.GetByNumber(context.Background(), repoID, pr.Number)
	if err != nil {
		t.Fatalf("GetByNumber: %v", err)
	}
	if found.State != model.PRStateClosed {
		t.Errorf("want state closed, got %q", found.State)
	}
}

// TestPullStore_SetDraft_True verifies that SetDraft can mark a non-draft pull
// request as a draft and the flag is persisted.
func TestPullStore_SetDraft_True(t *testing.T) {
	ps, repoID, ownerID := seedPullDeps(t)

	pr := &model.PullRequest{
		RepoID:     repoID,
		AuthorID:   ownerID,
		Title:      "Draft PR",
		HeadBranch: "feature",
		BaseBranch: "main",
		State:      model.PRStateOpen,
		IsDraft:    false,
	}
	if err := ps.Create(context.Background(), pr); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := ps.SetDraft(context.Background(), pr.ID, true); err != nil {
		t.Fatalf("SetDraft: %v", err)
	}

	found, err := ps.GetByNumber(context.Background(), repoID, pr.Number)
	if err != nil {
		t.Fatalf("GetByNumber: %v", err)
	}
	if !found.IsDraft {
		t.Error("SetDraft(true) must set IsDraft=true")
	}
}

// TestPullStore_ListOpen_ReturnsOpenPRs verifies that ListOpen returns only open pull
// requests for a repository, excluding merged or closed ones.
func TestPullStore_ListOpen_ReturnsOpenPRs(t *testing.T) {
	ps, repoID, ownerID := seedPullDeps(t)

	open := &model.PullRequest{
		RepoID:     repoID,
		AuthorID:   ownerID,
		Title:      "Open PR",
		HeadBranch: "open-feature",
		BaseBranch: "main",
		State:      model.PRStateOpen,
	}
	closed := &model.PullRequest{
		RepoID:     repoID,
		AuthorID:   ownerID,
		Title:      "Closed PR",
		HeadBranch: "closed-feature",
		BaseBranch: "main",
		State:      model.PRStateOpen, // start open, then close
	}
	if err := ps.Create(context.Background(), open); err != nil {
		t.Fatalf("Create open: %v", err)
	}
	if err := ps.Create(context.Background(), closed); err != nil {
		t.Fatalf("Create closed: %v", err)
	}
	if err := ps.UpdateState(context.Background(), closed.ID, model.PRStateClosed); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}

	prs, err := ps.ListOpen(context.Background(), repoID)
	if err != nil {
		t.Fatalf("ListOpen: %v", err)
	}
	for _, pr := range prs {
		if pr.State != model.PRStateOpen {
			t.Errorf("ListOpen must return only open PRs, got state %q", pr.State)
		}
	}
	// The open PR must be in the results.
	found := false
	for _, pr := range prs {
		if pr.ID == open.ID {
			found = true
		}
	}
	if !found {
		t.Error("ListOpen must include the open PR we created")
	}
}
