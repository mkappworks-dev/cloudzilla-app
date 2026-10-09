package service_test

// Integration tests for ThreadSubscriptionService. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"database/sql"
	"slices"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type threadSubEnv struct {
	db      *sql.DB
	svc     *service.ThreadSubscriptionService
	watches *store.WatchStore
	suffix  string
	userID  int64
	repoID  int64
}

func newThreadSubEnv(t *testing.T) *threadSubEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, userID, "testuser_"+suffix, suffix)
	watches := store.NewWatchStore(db)
	return &threadSubEnv{
		db:      db,
		svc:     service.NewThreadSubscriptionService(store.NewThreadSubscriptionStore(db), watches),
		watches: watches,
		suffix:  suffix,
		userID:  userID,
		repoID:  repoID,
	}
}

func (e *threadSubEnv) status(t *testing.T) service.ThreadStatus {
	t.Helper()
	st, err := e.svc.Status(context.Background(), e.userID, e.repoID, model.ThreadKindIssue, 1)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	return st
}

func TestThreadSubscriptionService_Status_NotSubscribedWithoutRowOrWatch(t *testing.T) {
	e := newThreadSubEnv(t)

	if st := e.status(t); st.State != "" || st.Reason != "" {
		t.Errorf("Status = %+v, want empty", st)
	}
}

func TestThreadSubscriptionService_Status_WatchingRepoIsSubscribedWatching(t *testing.T) {
	e := newThreadSubEnv(t)
	if err := e.watches.Set(context.Background(), e.userID, e.repoID, model.WatchLevelWatching); err != nil {
		t.Fatalf("watch: %v", err)
	}

	st := e.status(t)

	if st.State != model.ThreadStateSubscribed || st.Reason != model.ThreadReasonWatching {
		t.Errorf("Status = %+v, want subscribed/watching", st)
	}
}

func TestThreadSubscriptionService_Status_OtherWatchLevelsDoNotSubscribe(t *testing.T) {
	for _, level := range []string{model.WatchLevelReleasesOnly, model.WatchLevelIgnoring} {
		t.Run(level, func(t *testing.T) {
			e := newThreadSubEnv(t)
			if err := e.watches.Set(context.Background(), e.userID, e.repoID, level); err != nil {
				t.Fatalf("watch: %v", err)
			}

			if st := e.status(t); st.State != "" {
				t.Errorf("Status = %+v, want not subscribed", st)
			}
		})
	}
}

func TestThreadSubscriptionService_Status_MutedBeatsRepoWatch(t *testing.T) {
	e := newThreadSubEnv(t)
	ctx := context.Background()
	if err := e.watches.Set(ctx, e.userID, e.repoID, model.WatchLevelWatching); err != nil {
		t.Fatalf("watch: %v", err)
	}
	if err := e.svc.Set(ctx, e.userID, e.repoID, model.ThreadKindIssue, 1, model.ThreadStateMuted); err != nil {
		t.Fatalf("Set: %v", err)
	}

	st := e.status(t)

	if st.State != model.ThreadStateMuted || st.Reason != model.ThreadReasonManual {
		t.Errorf("Status = %+v, want muted/manual", st)
	}
}

func TestThreadSubscriptionService_Status_SubscribedRowBeatsIgnoringWatch(t *testing.T) {
	e := newThreadSubEnv(t)
	ctx := context.Background()
	if err := e.watches.Set(ctx, e.userID, e.repoID, model.WatchLevelIgnoring); err != nil {
		t.Fatalf("watch: %v", err)
	}
	if err := e.svc.Set(ctx, e.userID, e.repoID, model.ThreadKindIssue, 1, model.ThreadStateSubscribed); err != nil {
		t.Fatalf("Set: %v", err)
	}

	st := e.status(t)

	if st.State != model.ThreadStateSubscribed || st.Reason != model.ThreadReasonManual {
		t.Errorf("Status = %+v, want subscribed/manual", st)
	}
}

func TestThreadSubscriptionService_AutoSubscribe_SubscribesWithReason(t *testing.T) {
	e := newThreadSubEnv(t)

	if err := e.svc.AutoSubscribe(context.Background(), e.userID, e.repoID, model.ThreadKindIssue, 1, model.ThreadReasonAuthor); err != nil {
		t.Fatalf("AutoSubscribe: %v", err)
	}

	if st := e.status(t); st.State != model.ThreadStateSubscribed || st.Reason != model.ThreadReasonAuthor {
		t.Errorf("Status = %+v, want subscribed/author", st)
	}
}

func TestThreadSubscriptionService_AutoSubscribe_DoesNotUndoAMute(t *testing.T) {
	e := newThreadSubEnv(t)
	ctx := context.Background()
	if err := e.svc.Set(ctx, e.userID, e.repoID, model.ThreadKindIssue, 1, model.ThreadStateMuted); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if err := e.svc.AutoSubscribe(ctx, e.userID, e.repoID, model.ThreadKindIssue, 1, model.ThreadReasonComment); err != nil {
		t.Fatalf("AutoSubscribe: %v", err)
	}

	if st := e.status(t); st.State != model.ThreadStateMuted || st.Reason != model.ThreadReasonManual {
		t.Errorf("Status = %+v, want the existing muted/manual", st)
	}
}

func TestThreadSubscriptionService_Set_OverwritesAnAutoSubscription(t *testing.T) {
	e := newThreadSubEnv(t)
	ctx := context.Background()
	if err := e.svc.AutoSubscribe(ctx, e.userID, e.repoID, model.ThreadKindIssue, 1, model.ThreadReasonAuthor); err != nil {
		t.Fatalf("AutoSubscribe: %v", err)
	}

	if err := e.svc.Set(ctx, e.userID, e.repoID, model.ThreadKindIssue, 1, model.ThreadStateMuted); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if st := e.status(t); st.State != model.ThreadStateMuted || st.Reason != model.ThreadReasonManual {
		t.Errorf("Status = %+v, want muted/manual", st)
	}
}

func TestThreadSubscriptionService_SubscribeOnMention_BreaksThroughAMute(t *testing.T) {
	e := newThreadSubEnv(t)
	ctx := context.Background()
	if err := e.svc.Set(ctx, e.userID, e.repoID, model.ThreadKindIssue, 1, model.ThreadStateMuted); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if err := e.svc.SubscribeOnMention(ctx, e.userID, e.repoID, model.ThreadKindIssue, 1); err != nil {
		t.Fatalf("SubscribeOnMention: %v", err)
	}

	if st := e.status(t); st.State != model.ThreadStateSubscribed || st.Reason != model.ThreadReasonMention {
		t.Errorf("Status = %+v, want subscribed/mention", st)
	}
}

func TestThreadSubscriptionService_Set_RejectsInvalidInput(t *testing.T) {
	e := newThreadSubEnv(t)
	ctx := context.Background()

	if err := e.svc.Set(ctx, e.userID, e.repoID, model.ThreadKindIssue, 1, "watching"); err == nil {
		t.Error("Set accepted state \"watching\"")
	}
	if err := e.svc.Set(ctx, e.userID, e.repoID, "commit", 1, model.ThreadStateMuted); err == nil {
		t.Error("Set accepted kind \"commit\"")
	}
	if err := e.svc.AutoSubscribe(ctx, e.userID, e.repoID, "commit", 1, model.ThreadReasonAuthor); err == nil {
		t.Error("AutoSubscribe accepted kind \"commit\"")
	}
	if err := e.svc.SubscribeOnMention(ctx, e.userID, e.repoID, "commit", 1); err == nil {
		t.Error("SubscribeOnMention accepted kind \"commit\"")
	}
}

func TestThreadSubscriptionService_SubscribedUsersAndMutedUsers(t *testing.T) {
	e := newThreadSubEnv(t)
	ctx := context.Background()
	muter := testutil.SeedUser(t, e.db, "mute_"+e.suffix)
	if err := e.svc.AutoSubscribe(ctx, e.userID, e.repoID, model.ThreadKindPull, 4, model.ThreadReasonReview); err != nil {
		t.Fatalf("AutoSubscribe: %v", err)
	}
	if err := e.svc.Set(ctx, muter, e.repoID, model.ThreadKindPull, 4, model.ThreadStateMuted); err != nil {
		t.Fatalf("Set: %v", err)
	}

	subscribed, err := e.svc.SubscribedUsers(ctx, e.repoID, model.ThreadKindPull, 4)
	if err != nil {
		t.Fatalf("SubscribedUsers: %v", err)
	}
	if !slices.Equal(subscribed, []int64{e.userID}) {
		t.Errorf("SubscribedUsers = %v, want [%d]", subscribed, e.userID)
	}
	muted, err := e.svc.MutedUsers(ctx, e.repoID, model.ThreadKindPull, 4)
	if err != nil {
		t.Fatalf("MutedUsers: %v", err)
	}
	if !slices.Equal(muted, []int64{muter}) {
		t.Errorf("MutedUsers = %v, want [%d]", muted, muter)
	}
}
