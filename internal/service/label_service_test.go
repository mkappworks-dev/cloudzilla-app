package service_test

// Integration tests for LabelService. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// newLabelSvc builds a LabelService backed by the test database and seeds an owner
// and public repo. Returns the service, ownerName, and repoName.
func newLabelSvc(t *testing.T) (*service.LabelService, string, string) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoName := "testrepo_" + suffix
	testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	svc := service.NewLabelService(
		store.NewLabelStore(db),
		store.NewRepoStore(db),
		store.NewIssueStore(db),
		store.NewPullStore(db),
		store.NewDiscussionStore(db),
	)
	return svc, ownerName, repoName
}

// TestLabelService_Create_AssignsID verifies that Create inserts a label for a
// repository and returns it with a non-zero ID.
func TestLabelService_Create_AssignsID(t *testing.T) {
	svc, owner, repo := newLabelSvc(t)

	label, err := svc.Create(context.Background(), owner, repo, "bug", "#ee0701", "Something is not working")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if label.ID == 0 {
		t.Error("Create must return a label with non-zero ID")
	}
	if label.Name != "bug" {
		t.Errorf("want name %q, got %q", "bug", label.Name)
	}
}

// TestLabelService_ListByRepo_ReturnsCreatedLabel verifies that ListByRepo returns all
// labels that were created for the repository.
func TestLabelService_ListByRepo_ReturnsCreatedLabel(t *testing.T) {
	svc, owner, repo := newLabelSvc(t)

	if _, err := svc.Create(context.Background(), owner, repo, "enhancement", "#84b6eb", "New feature"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	labels, err := svc.ListByRepo(context.Background(), owner, repo)
	if err != nil {
		t.Fatalf("ListByRepo: %v", err)
	}
	if len(labels) == 0 {
		t.Error("ListByRepo must return at least the label we created")
	}
}

// TestLabelService_Delete_RemovesLabel verifies that Delete removes a label so that
// it no longer appears in ListByRepo.
func TestLabelService_Delete_RemovesLabel(t *testing.T) {
	svc, owner, repo := newLabelSvc(t)

	label, err := svc.Create(context.Background(), owner, repo, "wontfix", "#ffffff", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.Delete(context.Background(), owner, repo, label.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	labels, err := svc.ListByRepo(context.Background(), owner, repo)
	if err != nil {
		t.Fatalf("ListByRepo after delete: %v", err)
	}
	for _, l := range labels {
		if l.ID == label.ID {
			t.Error("deleted label must not appear in ListByRepo")
		}
	}
}

// TestLabelService_Create_DuplicateName_Error verifies that creating two labels with
// the same name in the same repository returns an error (labels must be unique per repo).
func TestLabelService_Create_DuplicateName_Error(t *testing.T) {
	svc, owner, repo := newLabelSvc(t)

	if _, err := svc.Create(context.Background(), owner, repo, "duplicate", "#000000", ""); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	_, err := svc.Create(context.Background(), owner, repo, "duplicate", "#ffffff", "")
	if err == nil {
		t.Error("Create must return an error for a duplicate label name in the same repo")
	}
}
