package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func (e *notifAccessEnv) publicRepo(t *testing.T, ownerID int64) model.Repository {
	t.Helper()
	id := testutil.SeedRepo(t, e.db, ownerID, "testuser_owner_"+e.suffix, e.suffix)
	return model.Repository{ID: id, OwnerID: ownerID, OwnerName: "testuser_owner_" + e.suffix, Name: "testrepo_" + e.suffix}
}

func (e *notifAccessEnv) setThread(t *testing.T, userID, repoID int64, kind string, number int64, state string) {
	t.Helper()
	if err := e.threads.Set(context.Background(), userID, repoID, kind, number, state); err != nil {
		t.Fatalf("set thread %s: %v", state, err)
	}
}

func (e *notifAccessEnv) mute(t *testing.T, userID, repoID int64, kind string, number int64) {
	t.Helper()
	e.setThread(t, userID, repoID, kind, number, model.ThreadStateMuted)
}

func (e *notifAccessEnv) subscribe(t *testing.T, userID, repoID int64, kind string, number int64) {
	t.Helper()
	e.setThread(t, userID, repoID, kind, number, model.ThreadStateSubscribed)
}

// subjectKinds lists userID's notification kinds oldest first, with "<null>" for none.
func (e *notifAccessEnv) subjectKinds(t *testing.T, userID int64) []string {
	t.Helper()
	rows, err := e.db.Query(`SELECT COALESCE(subject_kind, '<null>') FROM notifications WHERE user_id = $1 ORDER BY id`, userID)
	if err != nil {
		t.Fatalf("subject kinds: %v", err)
	}
	defer rows.Close()
	var kinds []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatalf("scan: %v", err)
		}
		kinds = append(kinds, k)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("subject kinds: %v", err)
	}
	return kinds
}

func TestNotification_MutedWatcher_GetsNothing(t *testing.T) {
	env := newNotifAccessEnv(t)
	ctx := context.Background()
	ownerID := env.seedUser(t, "owner")
	mutedID := env.seedUser(t, "muted")
	watcherID := env.seedUser(t, "watcher")
	repo := env.publicRepo(t, ownerID)
	env.watch(t, repo.ID, mutedID, watcherID)

	issue := model.Issue{Number: 1, AuthorID: ownerID, State: model.IssueStateClosed}
	pr := model.PullRequest{Number: 2, AuthorID: ownerID, State: model.PRStateMerged}
	env.mute(t, mutedID, repo.ID, model.ThreadKindIssue, 1)
	env.mute(t, mutedID, repo.ID, model.ThreadKindPull, 2)

	env.svc.NotifyIssueComment(ctx, repo, issue, env.actorID, "actor")
	env.svc.NotifyIssueStateChange(ctx, repo, issue, env.actorID, "actor")
	env.svc.NotifyPRComment(ctx, repo, pr, env.actorID, "actor")
	env.svc.NotifyPRReview(ctx, repo, pr, env.actorID, "actor")
	env.svc.NotifyPRStateChange(ctx, repo, pr, env.actorID, "actor")

	if got := env.count(t, mutedID); got != 0 {
		t.Errorf("muted watcher got %d notifications, want 0", got)
	}
	if got := env.count(t, watcherID); got != 5 {
		t.Errorf("unmuted watcher got %d notifications, want 5", got)
	}
}

func TestNotification_MuteIsPerThread(t *testing.T) {
	env := newNotifAccessEnv(t)
	ownerID := env.seedUser(t, "owner")
	mutedID := env.seedUser(t, "muted")
	repo := env.publicRepo(t, ownerID)
	env.watch(t, repo.ID, mutedID)
	env.mute(t, mutedID, repo.ID, model.ThreadKindIssue, 1)

	env.svc.NotifyIssueComment(context.Background(), repo, model.Issue{Number: 2, AuthorID: ownerID}, env.actorID, "actor")

	if got := env.count(t, mutedID); got != 1 {
		t.Errorf("watcher muted on another thread got %d notifications, want 1", got)
	}
}

func TestNotification_Subscriber_NotifiedWithoutWatching(t *testing.T) {
	env := newNotifAccessEnv(t)
	ctx := context.Background()
	ownerID := env.seedUser(t, "owner")
	subID := env.seedUser(t, "sub")
	ignoringID := env.seedUser(t, "ignoring")
	repo := env.publicRepo(t, ownerID)
	if err := env.watches.Set(ctx, ignoringID, repo.ID, model.WatchLevelIgnoring); err != nil {
		t.Fatalf("ignore: %v", err)
	}
	env.subscribe(t, subID, repo.ID, model.ThreadKindPull, 3)
	env.subscribe(t, ignoringID, repo.ID, model.ThreadKindPull, 3)

	env.svc.NotifyPRComment(ctx, repo, model.PullRequest{Number: 3, AuthorID: ownerID}, env.actorID, "actor")

	for name, id := range map[string]int64{"non-watcher": subID, "ignoring watcher": ignoringID} {
		if got := env.count(t, id); got != 1 {
			t.Errorf("subscribed %s got %d notifications, want 1", name, got)
		}
	}
}

func TestNotification_Subscriber_OnlyForTheirThread(t *testing.T) {
	env := newNotifAccessEnv(t)
	ownerID := env.seedUser(t, "owner")
	subID := env.seedUser(t, "sub")
	repo := env.publicRepo(t, ownerID)
	env.subscribe(t, subID, repo.ID, model.ThreadKindIssue, 3)

	env.svc.NotifyPRComment(context.Background(), repo, model.PullRequest{Number: 3, AuthorID: ownerID}, env.actorID, "actor")

	if got := env.count(t, subID); got != 0 {
		t.Errorf("issue subscriber got %d notifications for a PR with the same number, want 0", got)
	}
}

func TestNotification_Subscriber_WithoutReadAccess_NotNotified(t *testing.T) {
	env := newNotifAccessEnv(t)
	ownerID := env.seedUser(t, "owner")
	formerID := env.seedUser(t, "former")
	repo := env.privateRepo(t, ownerID)
	env.subscribe(t, formerID, repo.ID, model.ThreadKindIssue, 1)

	env.svc.NotifyIssueComment(context.Background(), repo, model.Issue{Number: 1, AuthorID: ownerID}, env.actorID, "actor")

	if got := env.count(t, formerID); got != 0 {
		t.Errorf("subscriber who can't read the repo got %d notifications, want 0", got)
	}
}

func TestNotification_Actor_StaysSilentEvenWhenSubscribed(t *testing.T) {
	env := newNotifAccessEnv(t)
	ownerID := env.seedUser(t, "owner")
	repo := env.publicRepo(t, ownerID)
	env.subscribe(t, env.actorID, repo.ID, model.ThreadKindIssue, 1)

	env.svc.NotifyIssueComment(context.Background(), repo, model.Issue{Number: 1, AuthorID: ownerID}, env.actorID, "actor")

	if got := env.count(t, env.actorID); got != 0 {
		t.Errorf("actor got %d notifications, want 0", got)
	}
}

func TestNotification_Author(t *testing.T) {
	env := newNotifAccessEnv(t)
	ctx := context.Background()
	ownerID := env.seedUser(t, "owner")
	repo := env.publicRepo(t, ownerID)
	mutedAuthor := env.seedUser(t, "mutedauthor")
	plainAuthor := env.seedUser(t, "plainauthor")
	env.mute(t, mutedAuthor, repo.ID, model.ThreadKindIssue, 1)

	env.svc.NotifyIssueComment(ctx, repo, model.Issue{Number: 1, AuthorID: mutedAuthor}, env.actorID, "actor")
	env.svc.NotifyIssueComment(ctx, repo, model.Issue{Number: 2, AuthorID: plainAuthor}, env.actorID, "actor")

	if got := env.count(t, mutedAuthor); got != 0 {
		t.Errorf("muted author got %d notifications, want 0", got)
	}
	if got := env.count(t, plainAuthor); got != 1 {
		t.Errorf("author with no thread row got %d notifications, want 1", got)
	}
}

func TestNotification_Mention_BreaksThroughMuteAndResubscribes(t *testing.T) {
	env := newNotifAccessEnv(t)
	ctx := context.Background()
	ownerID := env.seedUser(t, "owner")
	mutedID := env.seedUser(t, "muted")
	repo := env.publicRepo(t, ownerID)
	env.mute(t, mutedID, repo.ID, model.ThreadKindPull, 4)

	env.svc.NotifyMention(ctx, repo, env.actorID, "actor", mutedID, model.ThreadKindPull, 4, "/o/r/pulls/4")

	if got := env.count(t, mutedID); got != 1 {
		t.Errorf("muted user got %d mention notifications, want 1", got)
	}
	st, err := env.threads.Status(ctx, mutedID, repo.ID, model.ThreadKindPull, 4)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != model.ThreadStateSubscribed || st.Reason != model.ThreadReasonMention {
		t.Errorf("status after mention = %+v, want subscribed/mention", st)
	}
}

func TestNotification_Mention_WithoutReadAccess_DoesNotSubscribe(t *testing.T) {
	env := newNotifAccessEnv(t)
	ctx := context.Background()
	ownerID := env.seedUser(t, "owner")
	outsiderID := env.seedUser(t, "outsider")
	repo := env.privateRepo(t, ownerID)

	env.svc.NotifyMention(ctx, repo, env.actorID, "actor", outsiderID, model.ThreadKindIssue, 1, "/o/r/issues/1")

	st, err := env.threads.Status(ctx, outsiderID, repo.ID, model.ThreadKindIssue, 1)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != "" {
		t.Errorf("status = %+v, want none", st)
	}
}

func TestNotification_DiscussionReply_NotifiesSubscribersNotWatchers(t *testing.T) {
	env := newNotifAccessEnv(t)
	ctx := context.Background()
	ownerID := env.seedUser(t, "owner")
	subID := env.seedUser(t, "sub")
	watcherID := env.seedUser(t, "watcher")
	mutedAuthor := env.seedUser(t, "mutedauthor")
	repo := env.publicRepo(t, ownerID)
	env.watch(t, repo.ID, watcherID)
	env.subscribe(t, subID, repo.ID, model.ThreadKindDiscussion, 7)
	env.mute(t, mutedAuthor, repo.ID, model.ThreadKindDiscussion, 8)

	env.svc.NotifyDiscussionReply(ctx, repo, model.Discussion{Number: 7, AuthorID: ownerID}, env.actorID, "actor")
	env.svc.NotifyDiscussionReply(ctx, repo, model.Discussion{Number: 8, AuthorID: mutedAuthor}, env.actorID, "actor")

	if got := env.count(t, subID); got != 1 {
		t.Errorf("discussion subscriber got %d notifications, want 1", got)
	}
	if got := env.count(t, ownerID); got != 1 {
		t.Errorf("discussion author got %d notifications, want 1", got)
	}
	if got := env.count(t, watcherID); got != 0 {
		t.Errorf("repo watcher got %d discussion notifications, want 0", got)
	}
	if got := env.count(t, mutedAuthor); got != 0 {
		t.Errorf("muted discussion author got %d notifications, want 0", got)
	}
}

func TestNotification_SetsSubjectKind(t *testing.T) {
	env := newNotifAccessEnv(t)
	ctx := context.Background()
	ownerID := env.seedUser(t, "owner")
	mentionedID := env.seedUser(t, "mentioned")
	repo := env.publicRepo(t, ownerID)

	env.svc.NotifyIssueComment(ctx, repo, model.Issue{Number: 1, AuthorID: ownerID}, env.actorID, "actor")
	env.svc.NotifyPRReview(ctx, repo, model.PullRequest{Number: 2, AuthorID: ownerID}, env.actorID, "actor")
	env.svc.NotifyDiscussionReply(ctx, repo, model.Discussion{Number: 3, AuthorID: ownerID}, env.actorID, "actor")
	env.svc.NotifyMention(ctx, repo, env.actorID, "actor", mentionedID, model.ThreadKindPull, 2, "/o/r/pulls/2")
	env.svc.NotifyRepoTransfer(ctx, model.RepoTransfer{ID: 9, RecipientID: mentionedID, RequesterID: env.actorID, RequesterName: "actor", RepoID: repo.ID, RepoName: repo.Name, OwnerName: repo.OwnerName})

	got := env.subjectKinds(t, ownerID)
	if len(got) != 3 || got[0] != "issue" || got[1] != "pull" || got[2] != "discussion" {
		t.Errorf("owner subject kinds = %v, want [issue pull discussion]", got)
	}
	got = env.subjectKinds(t, mentionedID)
	if len(got) != 2 || got[0] != "pull" || got[1] != "<null>" {
		t.Errorf("mentioned user subject kinds = %v, want [pull <null>]", got)
	}
}

func TestNotification_Email_GoesToAuthorAndSubscribersNotBareWatchers(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	env := newNotifAccessEnvWithSMTP(t, smtp)
	ownerID := env.seedUser(t, "owner")
	subID := env.seedUser(t, "sub")
	watcherID := env.seedUser(t, "watcher")
	repo := env.publicRepo(t, ownerID)
	env.watch(t, repo.ID, watcherID)
	env.subscribe(t, subID, repo.ID, model.ThreadKindIssue, 1)

	env.svc.NotifyIssueComment(context.Background(), repo, model.Issue{Number: 1, AuthorID: ownerID}, env.actorID, "actor")

	box.NextTo(t, "testuser_owner_"+env.suffix+"@test.invalid")
	box.NextTo(t, "testuser_sub_"+env.suffix+"@test.invalid")
	box.Empty(t, 300*time.Millisecond)
	if got := env.count(t, watcherID); got != 1 {
		t.Errorf("bare watcher got %d in-app notifications, want 1", got)
	}
}
