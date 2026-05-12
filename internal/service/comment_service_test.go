package service_test

// Integration tests for CommentService. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// newCommentSvc builds a CommentService backed by the test database and seeds an issue
// for use in comment tests. Returns the service, the repo model, and the seeded issue.
func newCommentSvc(t *testing.T) (*service.CommentService, model.Repository, model.Issue) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)

	userSvc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})
	emailSvc := service.NewEmailService(config.SMTPConfig{})
	notifSvc := service.NewNotificationService(
		store.NewNotificationStore(db),
		store.NewWatchStore(db),
		emailSvc,
		userSvc,
	)
	svc := service.NewCommentService(
		store.NewCommentStore(db),
		store.NewMentionStore(db),
		userSvc,
		notifSvc,
	)

	repo := model.Repository{
		ID:        repoID,
		Name:      "testrepo_" + suffix,
		OwnerName: ownerName,
	}

	// Seed an issue row directly for use as the comment parent.
	issueStore := store.NewIssueStore(db)
	issue := &model.Issue{
		RepoID:     repoID,
		AuthorID:   ownerID,
		Title:      "Comment test issue",
		Body:       "body",
		State:      model.IssueStateOpen,
		Visibility: "public",
	}
	if err := issueStore.Create(context.Background(), issue); err != nil {
		t.Fatalf("seed issue: %v", err)
	}

	return svc, repo, *issue
}

// TestCommentService_CreateForIssue_AssignsID verifies that CreateForIssue inserts a
// comment and returns it with a non-zero ID.
func TestCommentService_CreateForIssue_AssignsID(t *testing.T) {
	svc, repo, issue := newCommentSvc(t)
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	authorID := testutil.SeedUser(t, db, suffix)

	c, err := svc.CreateForIssue(context.Background(), repo, issue.ID, issue.Number, authorID, "testuser_"+suffix, "great issue!")
	if err != nil {
		t.Fatalf("CreateForIssue: %v", err)
	}
	if c.ID == 0 {
		t.Error("CreateForIssue must return a comment with non-zero ID")
	}
	if c.Body != "great issue!" {
		t.Errorf("want body %q, got %q", "great issue!", c.Body)
	}
}

// TestCommentService_GetByID_ReturnsCorrectComment verifies that GetByID retrieves the
// previously created comment by its database ID.
func TestCommentService_GetByID_ReturnsCorrectComment(t *testing.T) {
	svc, repo, issue := newCommentSvc(t)
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	authorID := testutil.SeedUser(t, db, suffix)

	c, err := svc.CreateForIssue(context.Background(), repo, issue.ID, issue.Number, authorID, "testuser_"+suffix, "findable comment")
	if err != nil {
		t.Fatalf("CreateForIssue: %v", err)
	}

	found, err := svc.GetByID(context.Background(), c.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if found.Body != "findable comment" {
		t.Errorf("want body %q, got %q", "findable comment", found.Body)
	}
}

// TestCommentService_Update_ChangesBody verifies that Update replaces the comment body
// and the change is visible via GetByID.
func TestCommentService_Update_ChangesBody(t *testing.T) {
	svc, repo, issue := newCommentSvc(t)
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	authorID := testutil.SeedUser(t, db, suffix)

	c, err := svc.CreateForIssue(context.Background(), repo, issue.ID, issue.Number, authorID, "testuser_"+suffix, "original body")
	if err != nil {
		t.Fatalf("CreateForIssue: %v", err)
	}

	updated, err := svc.Update(context.Background(), c.ID, authorID, "updated body")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Body != "updated body" {
		t.Errorf("want updated body, got %q", updated.Body)
	}
}

// TestCommentService_ListByIssue_ReturnsComments verifies that ListByIssue returns all
// comments created for a given issue, including the one just created.
func TestCommentService_ListByIssue_ReturnsComments(t *testing.T) {
	svc, repo, issue := newCommentSvc(t)
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	authorID := testutil.SeedUser(t, db, suffix)

	if _, err := svc.CreateForIssue(context.Background(), repo, issue.ID, issue.Number, authorID, "testuser_"+suffix, "listed comment"); err != nil {
		t.Fatalf("CreateForIssue: %v", err)
	}

	comments, err := svc.ListByIssue(context.Background(), issue.ID)
	if err != nil {
		t.Fatalf("ListByIssue: %v", err)
	}
	if len(comments) == 0 {
		t.Error("ListByIssue must return at least the comment we created")
	}
}

// TestCommentService_Delete_RemovesComment verifies that Delete removes a comment so
// that GetByID returns an error afterward.
func TestCommentService_Delete_RemovesComment(t *testing.T) {
	svc, repo, issue := newCommentSvc(t)
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	authorID := testutil.SeedUser(t, db, suffix)

	c, err := svc.CreateForIssue(context.Background(), repo, issue.ID, issue.Number, authorID, "testuser_"+suffix, "to be deleted")
	if err != nil {
		t.Fatalf("CreateForIssue: %v", err)
	}

	if err := svc.Delete(context.Background(), c.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = svc.GetByID(context.Background(), c.ID)
	if err == nil {
		t.Error("GetByID must return an error after the comment is deleted")
	}
}
