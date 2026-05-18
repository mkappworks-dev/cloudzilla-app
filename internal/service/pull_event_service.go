package service

import (
	"context"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

type PullEventService struct {
	events *store.PullEventStore
}

func NewPullEventService(events *store.PullEventStore) *PullEventService {
	return &PullEventService{events: events}
}

// Record writes a timeline event. A failed write must not fail the action that
// triggered it — callers log the returned error and continue.
func (s *PullEventService) Record(ctx context.Context, pullID, actorID int64, actorName, eventType, detail string) error {
	e := &model.PullEvent{
		PullID:    pullID,
		ActorID:   actorID,
		ActorName: actorName,
		Type:      eventType,
		Detail:    detail,
	}
	if err := s.events.Create(ctx, e); err != nil {
		return fmt.Errorf("record pull event %q: %w", eventType, err)
	}
	return nil
}

func (s *PullEventService) ListByPull(ctx context.Context, pullID int64) ([]model.PullEvent, error) {
	return s.events.ListByPull(ctx, pullID)
}
