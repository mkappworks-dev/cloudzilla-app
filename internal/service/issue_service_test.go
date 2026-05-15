package service_test

// Integration tests for IssueService. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// newIssueSvc builds an IssueService backed by the test database,
// seeding an owner and public repo. Returns the service, ownerID, and repoName.
func newIssueSvc(t *testing.T) (*service.IssueService, int64, string) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	repoStore := store.NewRepoStore(db)
	repoSvc := service.NewRepoService(
		repoStore,
		store.NewUserStore(db),
		store.NewOrgStore(db),
		nil,
		nil,
		nil,
		config.GitConfig{},
	)
	svc := service.NewIssueService(store.NewIssueStore(db), repoStore, store.NewPullStore(db), repoSvc)
	return svc, ownerID, ownerName + "/testrepo_" + suffix
}

// ownerAndRepo splits "owner/repo" into its two parts.
func ownerAndRepo(ownerRepo string) (string, string) {
	for i, c := range ownerRepo {
		if c == '/' {
			return ownerRepo[:i], ownerRepo[i+1:]
		}
	}
	return ownerRepo, ""
}

// TestIssueService_Create_PublicIssue verifies that the owner can create a public
// issue and it is returned with a non-zero ID and open state.
func TestIssueService_Create_PublicIssue(t *testing.T) {
	svc, ownerID, ownerRepo := newIssueSvc(t)
	owner, repo := ownerAndRepo(ownerRepo)

	issue, err := svc.Create(context.Background(), owner, repo, ownerID, "Test Issue", "body", "public")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if issue.ID == 0 {
		t.Error("created issue must have non-zero ID")
	}
	if issue.State != model.IssueStateOpen {
		t.Errorf("new issue must be open, got %q", issue.State)
	}
	if issue.Visibility != "public" {
		t.Errorf("want visibility %q, got %q", "public", issue.Visibility)
	}
}

// TestIssueService_Create_DefaultsToPublic verifies that an empty visibility string
// is treated as "public" (the safe default).
func TestIssueService_Create_DefaultsToPublic(t *testing.T) {
	svc, ownerID, ownerRepo := newIssueSvc(t)
	owner, repo := ownerAndRepo(ownerRepo)

	issue, err := svc.Create(context.Background(), owner, repo, ownerID, "Default vis", "body", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if issue.Visibility != "public" {
		t.Errorf("empty visibility must default to public, got %q", issue.Visibility)
	}
}

// TestIssueService_Create_PrivateIssue_OwnerAllowed verifies that the repo owner
// (who has write access) can create a private issue.
func TestIssueService_Create_PrivateIssue_OwnerAllowed(t *testing.T) {
	svc, ownerID, ownerRepo := newIssueSvc(t)
	owner, repo := ownerAndRepo(ownerRepo)

	issue, err := svc.Create(context.Background(), owner, repo, ownerID, "Private Issue", "body", "private")
	if err != nil {
		t.Fatalf("Create private issue: %v", err)
	}
	if issue.Visibility != "private" {
		t.Errorf("want private visibility, got %q", issue.Visibility)
	}
}

// TestIssueService_Create_UnknownRepo_Error verifies that Create returns an error
// when the repository does not exist.
func TestIssueService_Create_UnknownRepo_Error(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoStore := store.NewRepoStore(db)
	repoSvc := service.NewRepoService(repoStore, store.NewUserStore(db), store.NewOrgStore(db), nil, nil, nil, config.GitConfig{})
	svc := service.NewIssueService(store.NewIssueStore(db), repoStore, store.NewPullStore(db), repoSvc)

	_, err := svc.Create(context.Background(), "nobody", "nonexistent", ownerID, "title", "body", "public")
	if err == nil {
		t.Error("Create with unknown repo must return an error")
	}
}

// TestIssueService_List_ReturnsCreatedIssue verifies that List returns issues
// that were previously created in the repository.
func TestIssueService_List_ReturnsCreatedIssue(t *testing.T) {
	svc, ownerID, ownerRepo := newIssueSvc(t)
	owner, repo := ownerAndRepo(ownerRepo)

	_, err := svc.Create(context.Background(), owner, repo, ownerID, "Listed Issue", "body", "public")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	issues, err := svc.List(context.Background(), owner, repo, &ownerID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(issues) == 0 {
		t.Error("List must return at least the issue we just created")
	}
}

// TestIssueService_SetState_CloseAndReopen verifies that SetState correctly transitions
// an issue from open → closed, and then from closed → open.
func TestIssueService_SetState_CloseAndReopen(t *testing.T) {
	svc, ownerID, ownerRepo := newIssueSvc(t)
	owner, repo := ownerAndRepo(ownerRepo)

	issue, err := svc.Create(context.Background(), owner, repo, ownerID, "State Issue", "body", "public")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Close the issue.
	closed, err := svc.SetState(context.Background(), owner, repo, issue.Number, model.IssueStateClosed)
	if err != nil {
		t.Fatalf("SetState closed: %v", err)
	}
	if closed.State != model.IssueStateClosed {
		t.Errorf("want closed, got %q", closed.State)
	}

	// Reopen the issue.
	reopened, err := svc.SetState(context.Background(), owner, repo, issue.Number, model.IssueStateOpen)
	if err != nil {
		t.Fatalf("SetState reopen: %v", err)
	}
	if reopened.State != model.IssueStateOpen {
		t.Errorf("want open after reopen, got %q", reopened.State)
	}
}
