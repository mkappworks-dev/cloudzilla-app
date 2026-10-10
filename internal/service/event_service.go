package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// EventService records and retrieves activity feed events.
type EventService struct {
	events *store.EventStore
	users  *store.UserStore
	repos  *store.RepoStore
}

// NewEventService creates an EventService backed by the given stores.
func NewEventService(events *store.EventStore, users *store.UserStore, repos *store.RepoStore) *EventService {
	return &EventService{events: events, users: users, repos: repos}
}

// Record inserts an activity event; marshal and DB errors are logged, not returned.
func (s *EventService) Record(ctx context.Context, actorID int64, actorName string, repoID *int64, repoName, ownerName, eventType string, payload map[string]any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		raw = []byte("{}")
	}
	e := &model.Event{
		ActorID:   actorID,
		ActorName: actorName,
		RepoID:    repoID,
		RepoName:  repoName,
		OwnerName: ownerName,
		EventType: eventType,
		Payload:   raw,
	}
	if err := s.events.Record(ctx, e); err != nil {
		slog.Warn("event record failed", "event_type", eventType, "actor_id", actorID, "error", err)
	}
}

// RecordPush records a push activity event for a single branch update.
func (s *EventService) RecordPush(ctx context.Context, actorID int64, actorName string, repoID *int64, repoName, ownerName string, summary model.PushSummary) {
	s.Record(ctx, actorID, actorName, repoID, repoName, ownerName, model.EventPush, map[string]any{
		"branch":       summary.Branch,
		"commit_total": summary.CommitTotal,
		"commits":      summary.Commits,
	})
}

// Feed returns a paginated list of events for a user's activity feed.
// filter selects the scope: "yours" (events the user performed),
// "watching" (events from watched repos), or "all"/"" (the full personalised feed).
func (s *EventService) Feed(ctx context.Context, userID int, filter string, page, pageSize int) ([]model.Event, error) {
	events, _, err := s.FeedPage(ctx, userID, filter, page, pageSize)
	return events, err
}

// FeedPage is Feed plus whether a later page exists, found by reading one row past the page.
func (s *EventService) FeedPage(ctx context.Context, userID int, filter string, page, pageSize int) (events []model.Event, hasMore bool, err error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 30
	}
	limit, offset := pageSize+1, (page-1)*pageSize
	switch filter {
	case "yours":
		events, err = s.events.ListOwnActivity(ctx, int64(userID), limit, offset)
	case "watching":
		events, err = s.events.ListWatching(ctx, int64(userID), limit, offset)
	default:
		events, err = s.events.ListForFeed(ctx, int64(userID), limit, offset)
	}
	if err != nil {
		return nil, false, err
	}
	if len(events) > pageSize {
		return events[:pageSize], true, nil
	}
	return events, false, nil
}

// FeedCounts returns event totals keyed "all", "yours", "watching".
func (s *EventService) FeedCounts(ctx context.Context, userID int) (map[string]int, error) {
	return s.events.FeedCounts(ctx, int64(userID))
}

// RepoActivity returns paginated public activity for a repo.
func (s *EventService) RepoActivity(ctx context.Context, owner, repoName string, page, pageSize int) ([]model.Event, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 30
	}
	repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("repo not found: %w", err)
	}
	if repo.Private {
		return []model.Event{}, nil
	}
	return s.events.ListByRepo(ctx, repo.ID, page, pageSize)
}

// UserActivity returns paginated public activity for a user.
func (s *EventService) UserActivity(ctx context.Context, username string, page, pageSize int) ([]model.Event, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 15
	}
	user, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}
	return s.events.ListByActor(ctx, user.ID, page, pageSize)
}
