package store_test

// Integration tests for LabelStore. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"testing"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
	"github.com/mkappworks/cloudzilla/internal/testutil"
)

// seedLabelDeps seeds an owner user and repo, returning the label store and repo ID.
func seedLabelDeps(t *testing.T) (*store.LabelStore, int64) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	return store.NewLabelStore(db), repoID
}

// TestLabelStore_Create_AssignsID verifies that Create inserts a label and assigns
// a non-zero database ID.
func TestLabelStore_Create_AssignsID(t *testing.T) {
	ls, repoID := seedLabelDeps(t)

	label := &model.Label{
		RepoID: repoID,
		Name:   "bug",
		Color:  "#ee0701",
	}
	if err := ls.Create(context.Background(), label); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if label.ID == 0 {
		t.Error("Create must assign a non-zero ID")
	}
}

// TestLabelStore_ListByRepo_ReturnsCreatedLabel verifies that ListByRepo returns
// labels created for the repository.
func TestLabelStore_ListByRepo_ReturnsCreatedLabel(t *testing.T) {
	ls, repoID := seedLabelDeps(t)

	if err := ls.Create(context.Background(), &model.Label{RepoID: repoID, Name: "enhancement", Color: "#84b6eb"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	labels, err := ls.ListByRepo(context.Background(), repoID)
	if err != nil {
		t.Fatalf("ListByRepo: %v", err)
	}
	if len(labels) == 0 {
		t.Error("ListByRepo must return at least the label we created")
	}
}

// TestLabelStore_Delete_RemovesLabel verifies that Delete removes a label so it no
// longer appears in ListByRepo.
func TestLabelStore_Delete_RemovesLabel(t *testing.T) {
	ls, repoID := seedLabelDeps(t)

	label := &model.Label{RepoID: repoID, Name: "wontfix", Color: "#ffffff"}
	if err := ls.Create(context.Background(), label); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := ls.Delete(context.Background(), label.ID, repoID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	labels, _ := ls.ListByRepo(context.Background(), repoID)
	for _, l := range labels {
		if l.ID == label.ID {
			t.Error("deleted label must not appear in ListByRepo")
		}
	}
}

// TestLabelStore_AddToIssue_ThenListByIssue verifies that AddToIssue associates a
// label with an issue and ListByIssue returns it.
func TestLabelStore_AddToIssue_ThenListByIssue(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	ls := store.NewLabelStore(db)

	// Create a label.
	label := &model.Label{RepoID: repoID, Name: "testlabel", Color: "#000000"}
	if err := ls.Create(context.Background(), label); err != nil {
		t.Fatalf("Create label: %v", err)
	}

	// Seed an issue.
	is := store.NewIssueStore(db)
	issue := &model.Issue{RepoID: repoID, AuthorID: ownerID, Title: "Issue", State: model.IssueStateOpen, Visibility: "public"}
	if err := is.Create(context.Background(), issue); err != nil {
		t.Fatalf("Create issue: %v", err)
	}

	if err := ls.AddToIssue(context.Background(), issue.ID, label.ID); err != nil {
		t.Fatalf("AddToIssue: %v", err)
	}

	issueLabels, err := ls.ListByIssue(context.Background(), issue.ID)
	if err != nil {
		t.Fatalf("ListByIssue: %v", err)
	}
	found := false
	for _, l := range issueLabels {
		if l.ID == label.ID {
			found = true
		}
	}
	if !found {
		t.Error("label must appear in ListByIssue after AddToIssue")
	}
}

// TestLabelStore_RemoveFromIssue verifies that RemoveFromIssue disassociates a label
// from an issue so it no longer appears in ListByIssue.
func TestLabelStore_RemoveFromIssue(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	ls := store.NewLabelStore(db)

	label := &model.Label{RepoID: repoID, Name: "removeme", Color: "#000000"}
	if err := ls.Create(context.Background(), label); err != nil {
		t.Fatalf("Create label: %v", err)
	}
	is := store.NewIssueStore(db)
	issue := &model.Issue{RepoID: repoID, AuthorID: ownerID, Title: "Issue", State: model.IssueStateOpen, Visibility: "public"}
	if err := is.Create(context.Background(), issue); err != nil {
		t.Fatalf("Create issue: %v", err)
	}

	if err := ls.AddToIssue(context.Background(), issue.ID, label.ID); err != nil {
		t.Fatalf("AddToIssue: %v", err)
	}
	if err := ls.RemoveFromIssue(context.Background(), issue.ID, label.ID); err != nil {
		t.Fatalf("RemoveFromIssue: %v", err)
	}

	issueLabels, _ := ls.ListByIssue(context.Background(), issue.ID)
	for _, l := range issueLabels {
		if l.ID == label.ID {
			t.Error("removed label must not appear in ListByIssue")
		}
	}
}
