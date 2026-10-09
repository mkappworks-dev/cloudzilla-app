package store_test

// Integration tests for ThreadSubscriptionStore. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"database/sql"
	"slices"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type threadSubEnv struct {
	db     *sql.DB
	store  *store.ThreadSubscriptionStore
	userID int64
	repoID int64
}

func newThreadSubEnv(t *testing.T) *threadSubEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, userID, "testuser_"+suffix, suffix)
	return &threadSubEnv{db: db, store: store.NewThreadSubscriptionStore(db), userID: userID, repoID: repoID}
}

func TestThreadSubscriptionStore_Get_MissingRowIsNil(t *testing.T) {
	e := newThreadSubEnv(t)

	got, err := e.store.Get(context.Background(), e.userID, e.repoID, model.ThreadKindIssue, 1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Errorf("Get = %+v, want nil", got)
	}
}

func TestThreadSubscriptionStore_Upsert_InsertsThenOverwrites(t *testing.T) {
	e := newThreadSubEnv(t)
	ctx := context.Background()

	if err := e.store.Upsert(ctx, e.userID, e.repoID, model.ThreadKindPull, 7, model.ThreadStateSubscribed, model.ThreadReasonMention); err != nil {
		t.Fatalf("Upsert insert: %v", err)
	}
	if err := e.store.Upsert(ctx, e.userID, e.repoID, model.ThreadKindPull, 7, model.ThreadStateMuted, model.ThreadReasonManual); err != nil {
		t.Fatalf("Upsert overwrite: %v", err)
	}

	got, err := e.store.Get(ctx, e.userID, e.repoID, model.ThreadKindPull, 7)
	if err != nil || got == nil {
		t.Fatalf("Get = %v, %v", got, err)
	}
	if got.State != model.ThreadStateMuted || got.Reason != model.ThreadReasonManual {
		t.Errorf("row = %s/%s, want muted/manual", got.State, got.Reason)
	}
	if got.UpdatedAt.Before(got.CreatedAt) {
		t.Errorf("updated_at %v before created_at %v", got.UpdatedAt, got.CreatedAt)
	}
}

func TestThreadSubscriptionStore_InsertIfAbsent_InsertsWhenMissing(t *testing.T) {
	e := newThreadSubEnv(t)
	ctx := context.Background()

	if err := e.store.InsertIfAbsent(ctx, e.userID, e.repoID, model.ThreadKindIssue, 3, model.ThreadStateSubscribed, model.ThreadReasonAuthor); err != nil {
		t.Fatalf("InsertIfAbsent: %v", err)
	}

	got, err := e.store.Get(ctx, e.userID, e.repoID, model.ThreadKindIssue, 3)
	if err != nil || got == nil {
		t.Fatalf("Get = %v, %v", got, err)
	}
	if got.State != model.ThreadStateSubscribed || got.Reason != model.ThreadReasonAuthor {
		t.Errorf("row = %s/%s, want subscribed/author", got.State, got.Reason)
	}
}

func TestThreadSubscriptionStore_InsertIfAbsent_KeepsExistingRow(t *testing.T) {
	e := newThreadSubEnv(t)
	ctx := context.Background()
	if err := e.store.Upsert(ctx, e.userID, e.repoID, model.ThreadKindIssue, 3, model.ThreadStateMuted, model.ThreadReasonManual); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	if err := e.store.InsertIfAbsent(ctx, e.userID, e.repoID, model.ThreadKindIssue, 3, model.ThreadStateSubscribed, model.ThreadReasonComment); err != nil {
		t.Fatalf("InsertIfAbsent: %v", err)
	}

	got, err := e.store.Get(ctx, e.userID, e.repoID, model.ThreadKindIssue, 3)
	if err != nil || got == nil {
		t.Fatalf("Get = %v, %v", got, err)
	}
	if got.State != model.ThreadStateMuted || got.Reason != model.ThreadReasonManual {
		t.Errorf("row = %s/%s, want the existing muted/manual", got.State, got.Reason)
	}
}

func TestThreadSubscriptionStore_RowsAreScopedByKindAndNumber(t *testing.T) {
	e := newThreadSubEnv(t)
	ctx := context.Background()
	if err := e.store.Upsert(ctx, e.userID, e.repoID, model.ThreadKindIssue, 1, model.ThreadStateMuted, model.ThreadReasonManual); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	for _, other := range []struct {
		kind   string
		number int64
	}{{model.ThreadKindPull, 1}, {model.ThreadKindDiscussion, 1}, {model.ThreadKindIssue, 2}} {
		got, err := e.store.Get(ctx, e.userID, e.repoID, other.kind, other.number)
		if err != nil {
			t.Fatalf("Get %s #%d: %v", other.kind, other.number, err)
		}
		if got != nil {
			t.Errorf("Get %s #%d = %+v, want nil", other.kind, other.number, got)
		}
	}
}

func TestThreadSubscriptionStore_ListByThread_FiltersByState(t *testing.T) {
	e := newThreadSubEnv(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	subscriber := testutil.SeedUser(t, e.db, "sub_"+suffix)
	muter := testutil.SeedUser(t, e.db, "mute_"+suffix)
	elsewhere := testutil.SeedUser(t, e.db, "else_"+suffix)
	mustUpsert := func(user int64, number int64, state string) {
		t.Helper()
		if err := e.store.Upsert(ctx, user, e.repoID, model.ThreadKindIssue, number, state, model.ThreadReasonManual); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}
	mustUpsert(subscriber, 5, model.ThreadStateSubscribed)
	mustUpsert(muter, 5, model.ThreadStateMuted)
	mustUpsert(elsewhere, 6, model.ThreadStateSubscribed)

	subscribed, err := e.store.ListByThread(ctx, e.repoID, model.ThreadKindIssue, 5, model.ThreadStateSubscribed)
	if err != nil {
		t.Fatalf("ListByThread subscribed: %v", err)
	}
	if !slices.Equal(subscribed, []int64{subscriber}) {
		t.Errorf("subscribed = %v, want [%d]", subscribed, subscriber)
	}
	muted, err := e.store.ListByThread(ctx, e.repoID, model.ThreadKindIssue, 5, model.ThreadStateMuted)
	if err != nil {
		t.Fatalf("ListByThread muted: %v", err)
	}
	if !slices.Equal(muted, []int64{muter}) {
		t.Errorf("muted = %v, want [%d]", muted, muter)
	}
}

func TestThreadSubscriptionStore_RejectsValuesOutsideTheAllowedSets(t *testing.T) {
	e := newThreadSubEnv(t)
	ctx := context.Background()
	tests := []struct{ name, kind, state, reason string }{
		{"kind", "commit", model.ThreadStateSubscribed, model.ThreadReasonManual},
		{"state", model.ThreadKindIssue, "watching", model.ThreadReasonManual},
		{"reason", model.ThreadKindIssue, model.ThreadStateSubscribed, "because"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := e.store.Upsert(ctx, e.userID, e.repoID, tt.kind, 1, tt.state, tt.reason); err == nil {
				t.Error("Upsert accepted a value the CHECK constraint should reject")
			}
			if err := e.store.InsertIfAbsent(ctx, e.userID, e.repoID, tt.kind, 1, tt.state, tt.reason); err == nil {
				t.Error("InsertIfAbsent accepted a value the CHECK constraint should reject")
			}
		})
	}
}

func TestThreadSubscriptionStore_DeletingTheUserOrRepoRemovesRows(t *testing.T) {
	e := newThreadSubEnv(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	otherUser := testutil.SeedUser(t, e.db, "other_"+suffix)
	if err := e.store.Upsert(ctx, otherUser, e.repoID, model.ThreadKindIssue, 1, model.ThreadStateSubscribed, model.ThreadReasonManual); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := e.store.Upsert(ctx, e.userID, e.repoID, model.ThreadKindIssue, 1, model.ThreadStateSubscribed, model.ThreadReasonManual); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	testutil.DeleteUsers(t, e.db, otherUser)
	if got, err := e.store.Get(ctx, otherUser, e.repoID, model.ThreadKindIssue, 1); err != nil || got != nil {
		t.Errorf("after user delete: Get = %v, %v; want nil, nil", got, err)
	}

	testutil.Exec(t, e.db, `DELETE FROM repositories WHERE id = $1`, e.repoID)
	if got, err := e.store.Get(ctx, e.userID, e.repoID, model.ThreadKindIssue, 1); err != nil || got != nil {
		t.Errorf("after repo delete: Get = %v, %v; want nil, nil", got, err)
	}
}
