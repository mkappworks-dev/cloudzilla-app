package service_test

// Integration tests for NotificationService. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"testing"

	"github.com/mkappworks/cloudzilla/internal/config"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/service"
	"github.com/mkappworks/cloudzilla/internal/store"
	"github.com/mkappworks/cloudzilla/internal/testutil"
)

// newNotifSvc builds a NotificationService backed by the test database.
// Returns the service, a repo seeded for the given owner, and the ownerID.
func newNotifSvc(t *testing.T) (*service.NotificationService, model.Repository, int64) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)

	userSvc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})
	emailSvc := service.NewEmailService(config.SMTPConfig{})
	svc := service.NewNotificationService(
		store.NewNotificationStore(db),
		store.NewWatchStore(db),
		emailSvc,
		userSvc,
	)
	repo := model.Repository{
		ID:        repoID,
		Name:      "testrepo_" + suffix,
		OwnerName: ownerName,
	}
	return svc, repo, ownerID
}

// TestNotification_MarkRead_RemovesFromUnread verifies that MarkRead transitions a specific
// notification from unread to read, reducing the unread count.
func TestNotification_MarkRead_RemovesFromUnread(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	actorID := testutil.SeedUser(t, db, "actor_"+suffix)

	userSvc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})
	notifStore := store.NewNotificationStore(db)
	svc := service.NewNotificationService(notifStore, store.NewWatchStore(db), service.NewEmailService(config.SMTPConfig{}), userSvc)

	// Create a notification directly via the store so we have a known ID.
	n := &model.Notification{
		UserID:    ownerID,
		ActorID:   actorID,
		ActorName: "actor_" + suffix,
		Type:      model.NotifIssueComment,
		RepoID:    repoID,
		RepoName:  "testrepo_" + suffix,
		OwnerName: ownerName,
		SubjectID: 1,
	}
	if err := notifStore.Create(context.Background(), n); err != nil {
		t.Fatalf("Create notification: %v", err)
	}

	before, err := svc.CountUnread(context.Background(), ownerID)
	if err != nil {
		t.Fatalf("CountUnread before: %v", err)
	}
	if before == 0 {
		t.Fatal("want at least 1 unread notification before marking read")
	}

	if err := svc.MarkRead(context.Background(), n.ID, ownerID); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	after, err := svc.CountUnread(context.Background(), ownerID)
	if err != nil {
		t.Fatalf("CountUnread after: %v", err)
	}
	if after >= before {
		t.Errorf("unread count must decrease after MarkRead: before=%d after=%d", before, after)
	}
}

// TestNotification_MarkAllRead_ClearsAllUnread verifies that MarkAllRead sets every
// notification for a user to read, bringing the unread count to zero.
func TestNotification_MarkAllRead_ClearsAllUnread(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	actorID := testutil.SeedUser(t, db, "actor2_"+suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)

	notifStore := store.NewNotificationStore(db)
	svc := service.NewNotificationService(notifStore, store.NewWatchStore(db),
		service.NewEmailService(config.SMTPConfig{}),
		service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"}),
	)

	// Seed two unread notifications.
	for i := range 2 {
		n := &model.Notification{
			UserID:    ownerID,
			ActorID:   actorID,
			ActorName: "actor2_" + suffix,
			Type:      model.NotifIssueClosed,
			RepoID:    repoID,
			RepoName:  "testrepo_" + suffix,
			OwnerName: "testuser_" + suffix,
			SubjectID: int64(i + 1),
			}
		if err := notifStore.Create(context.Background(), n); err != nil {
			t.Fatalf("seed notification %d: %v", i, err)
		}
	}

	if err := svc.MarkAllRead(context.Background(), ownerID); err != nil {
		t.Fatalf("MarkAllRead: %v", err)
	}

	count, err := svc.CountUnread(context.Background(), ownerID)
	if err != nil {
		t.Fatalf("CountUnread: %v", err)
	}
	if count != 0 {
		t.Errorf("want 0 unread after MarkAllRead, got %d", count)
	}
}

// TestNotification_NotifyIssueComment_SelfAction_Silent verifies the self-action
// suppression rule: when the actor is also the issue author, no notification is created
// for the author (they should not receive notifications for their own actions).
func TestNotification_NotifyIssueComment_SelfAction_Silent(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	authorID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, authorID, ownerName, suffix)

	notifStore := store.NewNotificationStore(db)
	svc := service.NewNotificationService(notifStore, store.NewWatchStore(db),
		service.NewEmailService(config.SMTPConfig{}),
		service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"}),
	)

	before, _ := svc.CountUnread(context.Background(), authorID)

	// Actor and issue author are the same user — self-action must be suppressed.
	issue := model.Issue{
		ID:       1,
		Number:   1,
		AuthorID: authorID,
	}
	repo := model.Repository{
		ID:        repoID,
		Name:      "testrepo_" + suffix,
		OwnerName: ownerName,
	}
	svc.NotifyIssueComment(context.Background(), repo, issue, authorID, ownerName)

	after, _ := svc.CountUnread(context.Background(), authorID)
	if after > before {
		t.Error("self-action must not create a notification for the actor")
	}
}

// TestNotification_NotifyIssueComment_DifferentActor_CreatesNotification verifies that
// when a different user comments on an issue, the issue author receives a notification.
func TestNotification_NotifyIssueComment_DifferentActor_CreatesNotification(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	authorID := testutil.SeedUser(t, db, suffix)
	actorID := testutil.SeedUser(t, db, "commenter_"+suffix)
	ownerName := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, authorID, ownerName, suffix)

	notifStore := store.NewNotificationStore(db)
	svc := service.NewNotificationService(notifStore, store.NewWatchStore(db),
		service.NewEmailService(config.SMTPConfig{}),
		service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"}),
	)

	before, _ := svc.CountUnread(context.Background(), authorID)

	issue := model.Issue{
		ID:       2,
		Number:   2,
		AuthorID: authorID,
	}
	repo := model.Repository{
		ID:        repoID,
		Name:      "testrepo_" + suffix,
		OwnerName: ownerName,
	}
	// Actor is different from author — author should receive the notification.
	svc.NotifyIssueComment(context.Background(), repo, issue, actorID, "commenter_"+suffix)

	after, _ := svc.CountUnread(context.Background(), authorID)
	if after <= before {
		t.Error("issue author must receive a notification when a different user comments")
	}
}

// TestNotification_List_ReturnsCreatedNotifications verifies that List returns all
// notifications for a given user, including those just created.
func TestNotification_List_ReturnsCreatedNotifications(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	actorID := testutil.SeedUser(t, db, "lister_actor_"+suffix)
	repoID := testutil.SeedRepo(t, db, userID, "testuser_"+suffix, suffix)

	notifStore := store.NewNotificationStore(db)
	svc := service.NewNotificationService(notifStore, store.NewWatchStore(db),
		service.NewEmailService(config.SMTPConfig{}),
		service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"}),
	)

	n := &model.Notification{
		UserID:    userID,
		ActorID:   actorID,
		ActorName: "lister_actor_" + suffix,
		Type:      model.NotifPRComment,
		RepoID:    repoID,
		RepoName:  "testrepo_" + suffix,
		OwnerName: "testuser_" + suffix,
		SubjectID: 5,
	}
	if err := notifStore.Create(context.Background(), n); err != nil {
		t.Fatalf("seed: %v", err)
	}

	notifs, err := svc.List(context.Background(), userID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(notifs) == 0 {
		t.Error("List must return at least the notification we just created")
	}
}
