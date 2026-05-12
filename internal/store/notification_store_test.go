package store_test

// Integration tests for NotificationStore. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// seedNotifDeps seeds a user and repo and returns the notification store,
// userID, actorID, and repoID for use in notification tests.
func seedNotifDeps(t *testing.T) (*store.NotificationStore, int64, int64, int64, string, string) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	actorID := testutil.SeedUser(t, db, "actor_ns_"+suffix)
	ownerName := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, userID, ownerName, suffix)
	return store.NewNotificationStore(db), userID, actorID, repoID, ownerName, "testrepo_" + suffix
}

// makeNotif builds a minimal unread notification struct for the given user/actor/repo.
func makeNotif(userID, actorID, repoID int64, ownerName, repoName string) *model.Notification {
	return &model.Notification{
		UserID:    userID,
		ActorID:   actorID,
		ActorName: ownerName,
		Type:      model.NotifIssueComment,
		RepoID:    repoID,
		RepoName:  repoName,
		OwnerName: ownerName,
		SubjectID: 1,
	}
}

// TestNotificationStore_Create_AssignsID verifies that Create inserts a notification
// and assigns a non-zero database ID.
func TestNotificationStore_Create_AssignsID(t *testing.T) {
	ns, userID, actorID, repoID, owner, repo := seedNotifDeps(t)

	n := makeNotif(userID, actorID, repoID, owner, repo)
	if err := ns.Create(context.Background(), n); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if n.ID == 0 {
		t.Error("Create must assign a non-zero ID")
	}
}

// TestNotificationStore_ListByUser_ReturnsCreated verifies that ListByUser returns
// the notification we just created.
func TestNotificationStore_ListByUser_ReturnsCreated(t *testing.T) {
	ns, userID, actorID, repoID, owner, repo := seedNotifDeps(t)

	n := makeNotif(userID, actorID, repoID, owner, repo)
	if err := ns.Create(context.Background(), n); err != nil {
		t.Fatalf("Create: %v", err)
	}

	notifs, err := ns.ListByUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(notifs) == 0 {
		t.Error("ListByUser must return at least the notification we created")
	}
}

// TestNotificationStore_CountUnread_IncreasesOnCreate verifies that CountUnread
// increases after a new unread notification is created for the user.
func TestNotificationStore_CountUnread_IncreasesOnCreate(t *testing.T) {
	ns, userID, actorID, repoID, owner, repo := seedNotifDeps(t)

	before, err := ns.CountUnread(context.Background(), userID)
	if err != nil {
		t.Fatalf("CountUnread before: %v", err)
	}

	n := makeNotif(userID, actorID, repoID, owner, repo)
	if err := ns.Create(context.Background(), n); err != nil {
		t.Fatalf("Create: %v", err)
	}

	after, err := ns.CountUnread(context.Background(), userID)
	if err != nil {
		t.Fatalf("CountUnread after: %v", err)
	}
	if after <= before {
		t.Errorf("CountUnread must increase after creating an unread notification: before=%d after=%d", before, after)
	}
}

// TestNotificationStore_MarkRead_DecreasesCount verifies that MarkRead marks a single
// notification as read, reducing the unread count.
func TestNotificationStore_MarkRead_DecreasesCount(t *testing.T) {
	ns, userID, actorID, repoID, owner, repo := seedNotifDeps(t)

	n := makeNotif(userID, actorID, repoID, owner, repo)
	if err := ns.Create(context.Background(), n); err != nil {
		t.Fatalf("Create: %v", err)
	}
	before, _ := ns.CountUnread(context.Background(), userID)

	if err := ns.MarkRead(context.Background(), n.ID, userID); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	after, err := ns.CountUnread(context.Background(), userID)
	if err != nil {
		t.Fatalf("CountUnread after MarkRead: %v", err)
	}
	if after >= before {
		t.Errorf("CountUnread must decrease after MarkRead: before=%d after=%d", before, after)
	}
}

// TestNotificationStore_MarkAllRead_ZeroCount verifies that MarkAllRead sets all
// notifications for the user to read, bringing CountUnread to zero.
func TestNotificationStore_MarkAllRead_ZeroCount(t *testing.T) {
	ns, userID, actorID, repoID, owner, repo := seedNotifDeps(t)

	// Seed two unread notifications.
	for i := range 2 {
		n := makeNotif(userID, actorID, repoID, owner, repo)
		n.SubjectID = int64(i + 10)
		if err := ns.Create(context.Background(), n); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}

	if err := ns.MarkAllRead(context.Background(), userID); err != nil {
		t.Fatalf("MarkAllRead: %v", err)
	}

	count, err := ns.CountUnread(context.Background(), userID)
	if err != nil {
		t.Fatalf("CountUnread: %v", err)
	}
	if count != 0 {
		t.Errorf("CountUnread must be 0 after MarkAllRead, got %d", count)
	}
}
