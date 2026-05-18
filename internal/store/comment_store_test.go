package store_test

// Integration tests for CommentStore. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// seedCommentDeps seeds an owner, repo, and issue for comment tests.
// Returns the comment store, issue ID, author ID, and repo ID.
func seedCommentDeps(t *testing.T) (*store.CommentStore, int64, int64, int64) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)

	issueStore := store.NewIssueStore(db)
	issue := &model.Issue{
		RepoID:     repoID,
		AuthorID:   ownerID,
		Title:      "Parent issue",
		State:      model.IssueStateOpen,
		Visibility: "public",
	}
	if err := issueStore.Create(context.Background(), issue); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	return store.NewCommentStore(db), issue.ID, ownerID, repoID
}

// TestCommentStore_Create_AssignsID verifies that Create inserts a comment and returns
// it with a non-zero database ID.
func TestCommentStore_Create_AssignsID(t *testing.T) {
	cs, issueID, authorID, repoID := seedCommentDeps(t)

	c := &model.Comment{
		RepoID:     repoID,
		IssueID:    &issueID,
		AuthorID:   authorID,
		AuthorName: "tester",
		Body:       "hello world",
	}
	if err := cs.Create(context.Background(), c); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if c.ID == 0 {
		t.Error("Create must assign a non-zero ID")
	}
}

// TestCommentStore_GetByID_ReturnsCorrectComment verifies that GetByID retrieves the
// comment by its database ID with the correct body.
func TestCommentStore_GetByID_ReturnsCorrectComment(t *testing.T) {
	cs, issueID, authorID, repoID := seedCommentDeps(t)

	c := &model.Comment{RepoID: repoID, IssueID: &issueID, AuthorID: authorID, AuthorName: "tester", Body: "findable"}
	if err := cs.Create(context.Background(), c); err != nil {
		t.Fatalf("Create: %v", err)
	}

	found, err := cs.GetByID(context.Background(), c.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if found.Body != "findable" {
		t.Errorf("want body %q, got %q", "findable", found.Body)
	}
}

// TestCommentStore_GetByID_Unknown_Error verifies that GetByID returns an error when
// the requested comment ID does not exist.
func TestCommentStore_GetByID_Unknown_Error(t *testing.T) {
	cs, _, _, _ := seedCommentDeps(t)

	_, err := cs.GetByID(context.Background(), 9_999_999)
	if err == nil {
		t.Error("GetByID must return an error for an unknown comment ID")
	}
}

// TestCommentStore_Update_ChangesBody verifies that Update replaces the comment body
// and the returned comment reflects the new value.
func TestCommentStore_Update_ChangesBody(t *testing.T) {
	cs, issueID, authorID, repoID := seedCommentDeps(t)

	c := &model.Comment{RepoID: repoID, IssueID: &issueID, AuthorID: authorID, AuthorName: "tester", Body: "original"}
	if err := cs.Create(context.Background(), c); err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := cs.Update(context.Background(), c.ID, "updated body")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Body != "updated body" {
		t.Errorf("want body %q, got %q", "updated body", updated.Body)
	}
}

// TestCommentStore_ListByIssue_ReturnsComments verifies that ListByIssue returns all
// comments for the given issue, including the one just created.
func TestCommentStore_ListByIssue_ReturnsComments(t *testing.T) {
	cs, issueID, authorID, repoID := seedCommentDeps(t)

	c := &model.Comment{RepoID: repoID, IssueID: &issueID, AuthorID: authorID, AuthorName: "tester", Body: "listed"}
	if err := cs.Create(context.Background(), c); err != nil {
		t.Fatalf("Create: %v", err)
	}

	comments, err := cs.ListByIssue(context.Background(), issueID)
	if err != nil {
		t.Fatalf("ListByIssue: %v", err)
	}
	if len(comments) == 0 {
		t.Error("ListByIssue must return at least the comment we created")
	}
}

// TestCommentStore_Delete_RemovesRow verifies that Delete removes the comment so that
// GetByID returns an error afterward.
func TestCommentStore_Delete_RemovesRow(t *testing.T) {
	cs, issueID, authorID, repoID := seedCommentDeps(t)

	c := &model.Comment{RepoID: repoID, IssueID: &issueID, AuthorID: authorID, AuthorName: "tester", Body: "bye"}
	if err := cs.Create(context.Background(), c); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := cs.Delete(context.Background(), c.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := cs.GetByID(context.Background(), c.ID)
	if err == nil {
		t.Error("GetByID must return an error after the comment is deleted")
	}
}
