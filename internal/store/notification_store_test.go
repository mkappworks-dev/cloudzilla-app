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
	got, err := ns.ListThreadPage(ctx, userID, store.NotifFilterInbox, "", 10, 0)
	if err != nil || len(got) != 1 {
		t.Fatalf("ListThreadPage: %v, %d rows", err, len(got))
	}
	if got[0].SubjectTitle != "Fix the flaky test" {
		t.Errorf("SubjectTitle = %q", got[0].SubjectTitle)
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

func TestNotificationStore_ListThreadPage_OneRowPerThreadNewestFirst(t *testing.T) {
	ns, userID, actorID, repoID, owner, repo := seedNotifDeps(t)
	ctx := context.Background()

	mk := func(user int64, kind string, number int64) *model.Notification {
		n := makeNotif(user, actorID, repoID, owner, repo)
		n.SubjectKind, n.SubjectID = kind, number
		if err := ns.Create(ctx, n); err != nil {
			t.Fatalf("Create: %v", err)
		}
		return n
	}
	issue1a := mk(userID, model.ThreadKindIssue, 1)
	pull1 := mk(userID, model.ThreadKindPull, 1)
	soloA := mk(userID, "", 5)
	soloB := mk(userID, "", 5)
	issue1b := mk(userID, model.ThreadKindIssue, 1)
	mk(actorID, model.ThreadKindIssue, 1)
	if err := ns.MarkRead(ctx, issue1a.ID, userID); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	got, err := ns.ListThreadPage(ctx, userID, store.NotifFilterInbox, "", 10, 0)
	if err != nil {
		t.Fatalf("ListThreadPage: %v", err)
	}
	type row struct {
		id            int64
		count, unread int
	}
	var rows []row
	for _, g := range got {
		rows = append(rows, row{g.ID, g.Count, g.Unread})
	}
	// Rows without a thread never merge, even with the same number.
	want := []row{{issue1b.ID, 2, 1}, {soloB.ID, 1, 1}, {soloA.ID, 1, 1}, {pull1.ID, 1, 1}}
	if !slices.Equal(rows, want) {
		t.Errorf("groups = %+v, want %+v", rows, want)
	}

	if n, err := ns.CountThreads(ctx, userID, store.NotifFilterInbox, ""); err != nil || n != 4 {
		t.Errorf("CountThreads(inbox) = %d, %v; want 4", n, err)
	}

	page2, err := ns.ListThreadPage(ctx, userID, store.NotifFilterInbox, "", 2, 2)
	if err != nil || len(page2) != 2 || page2[0].ID != soloA.ID || page2[1].ID != pull1.ID {
		t.Errorf("page 2 = %+v, %v; want soloA, pull1", page2, err)
	}

	read, err := ns.ListThreadPage(ctx, userID, store.NotifFilterRead, "", 10, 0)
	if err != nil || len(read) != 1 || read[0].ID != issue1a.ID || read[0].Count != 1 {
		t.Errorf("read groups = %+v, %v; want only issue1a, counting its read notification", read, err)
	}
	if n, _ := ns.CountThreads(ctx, userID, store.NotifFilterUnread, ""); n != 4 {
		t.Errorf("CountThreads(unread) = %d; want 4", n)
	}
}

func TestNotificationStore_ListThreadPage_Reasons(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	actorID := testutil.SeedUser(t, db, "actor_nr_"+suffix)
	owner, repo := "testuser_"+suffix, "testrepo_"+suffix
	repoID := testutil.SeedRepo(t, db, userID, owner, suffix)
	ns := store.NewNotificationStore(db)
	ctx := context.Background()

	issue := func() *model.Issue {
		i := &model.Issue{RepoID: repoID, AuthorID: actorID, Title: "i", State: model.IssueStateOpen}
		if err := store.NewIssueStore(db).Create(ctx, i); err != nil {
			t.Fatalf("issue: %v", err)
		}
		return i
	}
	pull := func() *model.PullRequest {
		p := &model.PullRequest{RepoID: repoID, AuthorID: actorID, Title: "p", State: model.PRStateOpen, HeadBranch: "h", BaseBranch: "main"}
		if err := store.NewPullStore(db).Create(ctx, p); err != nil {
			t.Fatalf("pull: %v", err)
		}
		return p
	}
	notify := func(typ model.NotificationType, kind string, number int) int64 {
		n := makeNotif(userID, actorID, repoID, owner, repo)
		n.Type, n.SubjectKind, n.SubjectID = typ, kind, int64(number)
		if err := ns.Create(ctx, n); err != nil {
			t.Fatalf("Create: %v", err)
		}
		return n.ID
	}

	assignedIssue, assignedPull, otherIssue := issue(), pull(), issue()
	if err := store.NewAssigneeStore(db).AddToIssue(ctx, assignedIssue.ID, userID); err != nil {
		t.Fatal(err)
	}
	if err := store.NewAssigneeStore(db).AddToPull(ctx, assignedPull.ID, userID); err != nil {
		t.Fatal(err)
	}
	requested, reviewed := pull(), pull()
	reviews := store.NewPullReviewStore(db)
	if err := reviews.RequestReview(ctx, requested.ID, repoID, userID, "u"); err != nil {
		t.Fatal(err)
	}
	if err := reviews.Upsert(ctx, &model.PullReview{PullID: reviewed.ID, RepoID: repoID, AuthorID: userID, State: model.PRReviewApproved}); err != nil {
		t.Fatal(err)
	}
	subs := store.NewThreadSubscriptionStore(db)
	if err := subs.Upsert(ctx, userID, repoID, model.ThreadKindIssue, int64(otherIssue.Number), model.ThreadStateSubscribed, model.ThreadReasonComment); err != nil {
		t.Fatal(err)
	}
	if err := subs.Upsert(ctx, userID, repoID, model.ThreadKindPull, int64(reviewed.Number), model.ThreadStateMuted, model.ThreadReasonManual); err != nil {
		t.Fatal(err)
	}

	assignedIssueN := notify(model.NotifIssueComment, model.ThreadKindIssue, assignedIssue.Number)
	assignedPullN := notify(model.NotifPRComment, model.ThreadKindPull, assignedPull.Number)
	// An issue and a pull share numbers; only the pull is requested.
	notify(model.NotifIssueComment, model.ThreadKindIssue, requested.Number)
	requestedN := notify(model.NotifPRComment, model.ThreadKindPull, requested.Number)
	notify(model.NotifPRComment, model.ThreadKindPull, reviewed.Number)
	notify(model.NotifIssueComment, model.ThreadKindIssue, otherIssue.Number)
	mentionN := notify(model.NotifMention, model.ThreadKindIssue, otherIssue.Number)
	strayMentionN := notify(model.NotifMention, "", 99)

	for _, tc := range []struct {
		reason string
		want   []int64
	}{
		{store.NotifReasonMention, []int64{strayMentionN, mentionN}},
		{store.NotifReasonReviewRequested, []int64{requestedN}},
		{store.NotifReasonAssigned, []int64{assignedPullN, assignedIssueN}},
		{store.NotifReasonSubscribed, []int64{mentionN}},
	} {
		got, err := ns.ListThreadPage(ctx, userID, store.NotifFilterInbox, tc.reason, 10, 0)
		if err != nil {
			t.Fatalf("ListThreadPage(%s): %v", tc.reason, err)
		}
		var gotIDs []int64
		for _, g := range got {
			gotIDs = append(gotIDs, g.ID)
		}
		if !slices.Equal(gotIDs, tc.want) {
			t.Errorf("reason %s = %v, want %v", tc.reason, gotIDs, tc.want)
		}
		if n, err := ns.CountThreads(ctx, userID, store.NotifFilterInbox, tc.reason); err != nil || n != len(tc.want) {
			t.Errorf("CountThreads(%s) = %d, %v; want %d", tc.reason, n, err, len(tc.want))
		}
	}
}

func TestNotificationStore_MarkDone_ReadsTheThreadUpToTheSelectedRow(t *testing.T) {
	ns, userID, actorID, repoID, owner, repo := seedNotifDeps(t)
	ctx := context.Background()

	mk := func(user int64, kind string, number int64) *model.Notification {
		n := makeNotif(user, actorID, repoID, owner, repo)
		n.SubjectKind, n.SubjectID = kind, number
		if err := ns.Create(ctx, n); err != nil {
			t.Fatalf("Create: %v", err)
		}
		return n
	}
	older := mk(userID, model.ThreadKindIssue, 1)
	selected := mk(userID, model.ThreadKindIssue, 1)
	otherThread := mk(userID, model.ThreadKindIssue, 2)
	solo := mk(userID, "", 1)
	otherSolo := mk(userID, "", 1)
	theirs := mk(actorID, model.ThreadKindIssue, 1)
	newer := mk(userID, model.ThreadKindIssue, 1)
	outOfOrder := mk(userID, model.ThreadKindIssue, 1)
	if _, err := testutil.OpenTestDB(t).ExecContext(ctx, `UPDATE notifications SET created_at = CASE id WHEN $1 THEN '2020-01-01'::timestamptz WHEN $2 THEN '2021-01-01'::timestamptz WHEN $3 THEN '2022-01-01'::timestamptz WHEN $4 THEN '2019-01-01'::timestamptz END WHERE id IN ($1,$2,$3,$4)`, older.ID, selected.ID, newer.ID, outOfOrder.ID); err != nil {
		t.Fatal(err)
	}

	if err := ns.MarkDone(ctx, userID, []int64{selected.ID, solo.ID, theirs.ID}); err != nil {
		t.Fatalf("MarkDone: %v", err)
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
	want := []int64{otherThread.ID, otherSolo.ID, newer.ID}
	if !slices.Equal(left, want) {
		t.Errorf("unread after = %v, want %v: the selected rows and their threads' older notifications become read (older=%d)", left, want, older.ID)
	}
	if n, _ := ns.CountUnread(ctx, actorID); n != 1 {
		t.Errorf("other user's unread = %d, want 1 (untouched)", n)
	}
	if err := ns.MarkDone(ctx, userID, nil); err != nil {
		t.Errorf("empty ids: %v", err)
	}
}

func TestNotificationStore_MarkUnread_FlipsOwnRowsOnly(t *testing.T) {
	ns, userID, actorID, repoID, owner, repo := seedNotifDeps(t)
	ctx := context.Background()
	mine, theirs := makeNotif(userID, actorID, repoID, owner, repo), makeNotif(actorID, userID, repoID, owner, repo)
	for _, n := range []*model.Notification{mine, theirs} {
		if err := ns.Create(ctx, n); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	if err := ns.MarkAllRead(ctx, userID); err != nil {
		t.Fatal(err)
	}
	if err := ns.MarkAllRead(ctx, actorID); err != nil {
		t.Fatal(err)
	}

	if err := ns.MarkUnread(ctx, userID, []int64{mine.ID, theirs.ID}); err != nil {
		t.Fatalf("MarkUnread: %v", err)
	}
	if n, _ := ns.CountUnread(ctx, userID); n != 1 {
		t.Errorf("own unread = %d, want 1", n)
	}
	if n, _ := ns.CountUnread(ctx, actorID); n != 0 {
		t.Errorf("other user's unread = %d, want 0 (untouched)", n)
	}
	if err := ns.MarkUnread(ctx, userID, nil); err != nil {
		t.Errorf("empty ids: %v", err)
	}
}

func TestNotificationStore_ThreadHeadIDs_OnePerGroupNewestFirst(t *testing.T) {
	ns, userID, actorID, repoID, owner, repo := seedNotifDeps(t)
	ctx := context.Background()
	mk := func(user int64, kind string, number int64) *model.Notification {
		n := makeNotif(user, actorID, repoID, owner, repo)
		n.SubjectKind, n.SubjectID = kind, number
		if err := ns.Create(ctx, n); err != nil {
			t.Fatalf("Create: %v", err)
		}
		return n
	}
	mk(userID, model.ThreadKindIssue, 1)
	headOne := mk(userID, model.ThreadKindIssue, 1)
	headTwo := mk(userID, model.ThreadKindIssue, 2)
	soloA, soloB := mk(userID, "", 1), mk(userID, "", 1)
	mk(actorID, model.ThreadKindIssue, 1)

	got, err := ns.ThreadHeadIDs(ctx, userID, store.NotifFilterInbox, "")
	if err != nil {
		t.Fatalf("ThreadHeadIDs: %v", err)
	}
	want := []int64{soloB.ID, soloA.ID, headTwo.ID, headOne.ID}
	if !slices.Equal(got, want) {
		t.Errorf("heads = %v, want %v: one per thread, each row-less notification alone, newest first", got, want)
	}
	total, _ := ns.CountThreads(ctx, userID, store.NotifFilterInbox, "")
	if total != len(got) {
		t.Errorf("CountThreads = %d, heads = %d; want them to agree", total, len(got))
	}
	if err := ns.MarkDone(ctx, userID, []int64{headOne.ID}); err != nil {
		t.Fatal(err)
	}
	unreadHeads, _ := ns.ThreadHeadIDs(ctx, userID, store.NotifFilterUnread, "")
	if len(unreadHeads) != 3 {
		t.Errorf("unread heads = %v, want the read thread gone", unreadHeads)
	}
}
