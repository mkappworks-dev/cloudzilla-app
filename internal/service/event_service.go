package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

type EventService struct {
	events *store.EventStore
	users  *store.UserStore
	repos  *store.RepoStore
}

func NewEventService(events *store.EventStore, users *store.UserStore, repos *store.RepoStore) *EventService {
	return &EventService{events: events, users: users, repos: repos}
}

// Record marshals the payload map and inserts the event asynchronously.
// Callers should invoke as: go services.Event.Record(ctx, ...)
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

// Feed returns a paginated list of events for a user's personalised feed.
func (s *EventService) Feed(ctx context.Context, userID, page, pageSize int) ([]model.Event, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 30
	}
	return s.events.ListForFeed(ctx, int64(userID), page, pageSize)
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
