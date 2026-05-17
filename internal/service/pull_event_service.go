package service

import (
	"context"
	"log/slog"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// PullEventService records and reads pull request timeline events.
type PullEventService struct {
	events *store.PullEventStore
}

// NewPullEventService creates a PullEventService.
func NewPullEventService(events *store.PullEventStore) *PullEventService {
	return &PullEventService{events: events}
}

// Record writes a timeline event. A failure is logged, never propagated — a
// missing timeline entry must not fail the action that triggered it.
func (s *PullEventService) Record(ctx context.Context, pullID, actorID int64, actorName, eventType, detail string) {
	e := &model.PullEvent{
		PullID:    pullID,
		ActorID:   actorID,
		ActorName: actorName,
		Type:      eventType,
		Detail:    detail,
	}
	if err := s.events.Create(ctx, e); err != nil {
		slog.Error("pull event record failed", "pull_id", pullID, "type", eventType, "error", err)
	}
}

// ListByPull returns a pull request's timeline events oldest-first.
func (s *PullEventService) ListByPull(ctx context.Context, pullID int64) ([]model.PullEvent, error) {
	return s.events.ListByPull(ctx, pullID)
}
