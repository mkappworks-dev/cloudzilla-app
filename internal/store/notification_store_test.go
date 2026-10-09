package store_test

// Integration tests for NotificationStore. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"slices"
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

func TestNotificationStore_Create_AcceptsEveryType(t *testing.T) {
	ns, userID, actorID, repoID, owner, repo := seedNotifDeps(t)
	ctx := context.Background()

	for i, typ := range model.AllNotificationTypes {
		n := makeNotif(userID, actorID, repoID, owner, repo)
		n.Type = typ
		n.SubjectID = int64(i + 1)
		if err := ns.Create(ctx, n); err != nil {
			t.Errorf("Create %s: %v", typ, err)
		}
	}

	notifs, err := ns.ListByUser(ctx, userID)
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	stored := map[model.NotificationType]bool{}
	for _, n := range notifs {
		stored[n.Type] = true
	}
	for _, typ := range model.AllNotificationTypes {
		if !stored[typ] {
			t.Errorf("no %s notification stored", typ)
		}
	}
}

func TestNotificationStore_SubjectTitle_RoundTrips(t *testing.T) {
	ns, userID, actorID, repoID, owner, repo := seedNotifDeps(t)
	ctx := context.Background()

	n := makeNotif(userID, actorID, repoID, owner, repo)
	n.SubjectTitle = "Fix the flaky test"
	if err := ns.Create(ctx, n); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := ns.ListPage(ctx, userID, store.NotifFilterInbox, 10, 0)
	if err != nil || len(got) != 1 {
		t.Fatalf("ListPage: %v, %d rows", err, len(got))
	}
	if got[0].SubjectTitle != "Fix the flaky test" {
		t.Errorf("SubjectTitle = %q", got[0].SubjectTitle)
	}
}

func TestNotificationStore_MarkReadMany_OnlyOwn(t *testing.T) {
	ns, userID, actorID, repoID, owner, repo := seedNotifDeps(t)
	ctx := context.Background()

	mine := makeNotif(userID, actorID, repoID, owner, repo)
	theirs := makeNotif(actorID, userID, repoID, owner, repo)
	for _, n := range []*model.Notification{mine, theirs} {
		if err := ns.Create(ctx, n); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	if err := ns.MarkReadMany(ctx, userID, []int64{mine.ID, theirs.ID}); err != nil {
		t.Fatalf("MarkReadMany: %v", err)
	}
	if n, _ := ns.CountUnread(ctx, userID); n != 0 {
		t.Errorf("own unread = %d, want 0", n)
	}
	if n, _ := ns.CountUnread(ctx, actorID); n != 1 {
		t.Errorf("other user's unread = %d, want 1 (untouched)", n)
	}
	if err := ns.MarkReadMany(ctx, userID, nil); err != nil {
		t.Errorf("empty ids: %v", err)
	}
}

func TestNotificationStore_ThreadsOf_DistinctOwnAndClassified(t *testing.T) {
	ns, userID, actorID, repoID, owner, repo := seedNotifDeps(t)
	ctx := context.Background()

	mk := func(user int64, typ model.NotificationType, kind string, number int64) *model.Notification {
		n := makeNotif(user, actorID, repoID, owner, repo)
		n.Type, n.SubjectKind, n.SubjectID = typ, kind, number
		return n
	}
	first := mk(userID, model.NotifIssueComment, model.ThreadKindIssue, 7)
	again := mk(userID, model.NotifIssueClosed, model.ThreadKindIssue, 7)
	pull := mk(userID, model.NotifPRComment, model.ThreadKindPull, 7)
	unclassified := mk(userID, model.NotifMention, "", 9)
	transfer := mk(userID, model.NotifRepoTransfer, "", 3)
	theirs := mk(actorID, model.NotifIssueComment, model.ThreadKindIssue, 8)
	all := []*model.Notification{first, again, pull, unclassified, transfer, theirs}
	for _, n := range all {
		if err := ns.Create(ctx, n); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	ids := make([]int64, len(all))
	for i, n := range all {
		ids[i] = n.ID
	}

	got, err := ns.ThreadsOf(ctx, userID, ids)
	if err != nil {
		t.Fatalf("ThreadsOf: %v", err)
	}
	want := []store.NotificationThread{
		{RepoID: repoID, Kind: model.ThreadKindIssue, Number: 7},
		{RepoID: repoID, Kind: model.ThreadKindPull, Number: 7},
	}
	if !slices.Equal(got, want) {
		t.Errorf("ThreadsOf = %v, want %v: one per thread, none for other users, null-kind or transfer rows", got, want)
	}

	got, err = ns.ThreadsOf(ctx, userID, nil)
	if err != nil || len(got) != 0 {
		t.Errorf("ThreadsOf(no ids) = %v, %v; want none", got, err)
	}
}

func TestNotificationStore_MarkThreadsRead_CoversUnselectedSiblingsOnly(t *testing.T) {
	ns, userID, actorID, repoID, owner, repo := seedNotifDeps(t)
	ctx := context.Background()

	mk := func(user int64, kind string, number int64) *model.Notification {
		n := makeNotif(user, actorID, repoID, owner, repo)
		n.SubjectKind, n.SubjectID = kind, number
		return n
	}
	selected := mk(userID, model.ThreadKindIssue, 1)
	sibling := mk(userID, model.ThreadKindIssue, 1)
	otherThread := mk(userID, model.ThreadKindIssue, 2)
	otherKind := mk(userID, model.ThreadKindPull, 1)
	unclassified := mk(userID, "", 1)
	theirs := mk(actorID, model.ThreadKindIssue, 1)
	for _, n := range []*model.Notification{selected, sibling, otherThread, otherKind, unclassified, theirs} {
		if err := ns.Create(ctx, n); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	if err := ns.MarkThreadsRead(ctx, userID, []int64{selected.ID, theirs.ID, unclassified.ID}); err != nil {
		t.Fatalf("MarkThreadsRead: %v", err)
	}
	unread, err := ns.ListUnreadReadable(ctx, userID)
	if err != nil {
		t.Fatalf("ListUnreadReadable: %v", err)
	}
	var left []int64
	for _, n := range unread {
		left = append(left, n.ID)
	}
	slices.Sort(left)
	want := []int64{otherThread.ID, otherKind.ID, unclassified.ID}
	slices.Sort(want)
	if !slices.Equal(left, want) {
		t.Errorf("unread after = %v, want %v: only the selected thread's notifications become read", left, want)
	}
	if n, _ := ns.CountUnread(ctx, actorID); n != 1 {
		t.Errorf("other user's unread = %d, want 1 (untouched)", n)
	}
	if err := ns.MarkThreadsRead(ctx, userID, nil); err != nil {
		t.Errorf("empty ids: %v", err)
	}
}

func TestNotificationStore_ListPage_FilterAndOffset(t *testing.T) {
	ns, userID, actorID, repoID, owner, repo := seedNotifDeps(t)
	ctx := context.Background()

	var ids []int64
	for i := 1; i <= 5; i++ {
		n := makeNotif(userID, actorID, repoID, owner, repo)
		n.SubjectID = int64(i)
		if err := ns.Create(ctx, n); err != nil {
			t.Fatalf("Create: %v", err)
		}
		ids = append(ids, n.ID)
	}
	if err := ns.MarkRead(ctx, ids[0], userID); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	for _, tc := range []struct {
		filter string
		want   int
	}{
		{store.NotifFilterInbox, 5},
		{store.NotifFilterUnread, 4},
		{store.NotifFilterRead, 1},
		{"bogus", 5},
	} {
		got, err := ns.CountByFilter(ctx, userID, tc.filter)
		if err != nil || got != tc.want {
			t.Errorf("CountByFilter(%q) = %d, %v; want %d", tc.filter, got, err, tc.want)
		}
	}

	page2, err := ns.ListPage(ctx, userID, store.NotifFilterInbox, 2, 2)
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	// Newest first: ids[4], ids[3] | ids[2], ids[1] | ids[0].
	if len(page2) != 2 || page2[0].ID != ids[2] || page2[1].ID != ids[1] {
		t.Errorf("page 2 = %+v; want ids %d, %d", page2, ids[2], ids[1])
	}
}
