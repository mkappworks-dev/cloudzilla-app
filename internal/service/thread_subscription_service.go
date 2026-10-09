package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// ThreadStatus is a user's effective subscription to one thread. State is empty when not subscribed.
type ThreadStatus struct {
	State  string
	Reason string
}

// ThreadSubscriptionService manages per-thread notification subscriptions.
type ThreadSubscriptionService struct {
	threads *store.ThreadSubscriptionStore
	watches *store.WatchStore
}

// NewThreadSubscriptionService creates a ThreadSubscriptionService backed by the given stores.
func NewThreadSubscriptionService(threads *store.ThreadSubscriptionStore, watches *store.WatchStore) *ThreadSubscriptionService {
	return &ThreadSubscriptionService{threads: threads, watches: watches}
}

// Status returns the thread row if the user has one, else subscribed/watching when they watch
// the repo at the "watching" level, else the zero ThreadStatus. A muted row wins over a repo watch.
func (s *ThreadSubscriptionService) Status(ctx context.Context, userID, repoID int64, kind string, number int64) (ThreadStatus, error) {
	row, err := s.threads.Get(ctx, userID, repoID, kind, number)
	if err != nil {
		return ThreadStatus{}, err
	}
	if row != nil {
		return ThreadStatus{State: row.State, Reason: row.Reason}, nil
	}
	w, err := s.watches.Get(ctx, userID, repoID)
	if err != nil {
		return ThreadStatus{}, err
	}
	if w != nil && w.Level == model.WatchLevelWatching {
		return ThreadStatus{State: model.ThreadStateSubscribed, Reason: model.ThreadReasonWatching}, nil
	}
	return ThreadStatus{}, nil
}

// Set records the user's manual choice, replacing any existing row.
func (s *ThreadSubscriptionService) Set(ctx context.Context, userID, repoID int64, kind string, number int64, state string) error {
	if state != model.ThreadStateSubscribed && state != model.ThreadStateMuted {
		return fmt.Errorf("invalid thread subscription state: %q", state)
	}
	if err := checkThreadKind(kind); err != nil {
		return err
	}
	return s.threads.Upsert(ctx, userID, repoID, kind, number, state, model.ThreadReasonManual)
}

// AutoSubscribe subscribes the user unless they already have a row, so it never undoes a mute.
func (s *ThreadSubscriptionService) AutoSubscribe(ctx context.Context, userID, repoID int64, kind string, number int64, reason string) error {
	if err := checkThreadKind(kind); err != nil {
		return err
	}
	return s.threads.InsertIfAbsent(ctx, userID, repoID, kind, number, model.ThreadStateSubscribed, reason)
}

// SubscribeOnMention subscribes the user even over a mute: a mention is addressed to them.
func (s *ThreadSubscriptionService) SubscribeOnMention(ctx context.Context, userID, repoID int64, kind string, number int64) error {
	if err := checkThreadKind(kind); err != nil {
		return err
	}
	return s.threads.Upsert(ctx, userID, repoID, kind, number, model.ThreadStateSubscribed, model.ThreadReasonMention)
}

// SubscribedUsers returns the users with a subscribed row on the thread. Repo watchers without a row are not included.
func (s *ThreadSubscriptionService) SubscribedUsers(ctx context.Context, repoID int64, kind string, number int64) ([]int64, error) {
	return s.threads.ListByThread(ctx, repoID, kind, number, model.ThreadStateSubscribed)
}

// MutedUsers returns the users with a muted row on the thread.
func (s *ThreadSubscriptionService) MutedUsers(ctx context.Context, repoID int64, kind string, number int64) ([]int64, error) {
	return s.threads.ListByThread(ctx, repoID, kind, number, model.ThreadStateMuted)
}

// threadAutoSubscriber is the part of ThreadSubscriptionService that the services
// where users participate in a thread depend on.
type threadAutoSubscriber interface {
	AutoSubscribe(ctx context.Context, userID, repoID int64, kind string, number int64, reason string) error
}

// autoSubscribe records userID's participation in a thread. A failed write is
// logged, not returned: participating must not fail over a subscription.
func autoSubscribe(ctx context.Context, sub threadAutoSubscriber, userID, repoID int64, kind string, number int, reason string) {
	if sub == nil {
		return
	}
	if err := sub.AutoSubscribe(ctx, userID, repoID, kind, int64(number), reason); err != nil {
		slog.Error("failed to auto-subscribe", "user_id", userID, "repo_id", repoID, "kind", kind, "number", number, "reason", reason, "error", err)
	}
}

func checkThreadKind(kind string) error {
	switch kind {
	case model.ThreadKindIssue, model.ThreadKindPull, model.ThreadKindDiscussion:
		return nil
	}
	return fmt.Errorf("invalid thread kind: %q", kind)
}
