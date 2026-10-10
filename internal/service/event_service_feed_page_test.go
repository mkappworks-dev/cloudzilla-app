package service_test

import (
	"context"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

func TestEventService_FeedPage_ReportsWhetherAnotherPageFollows(t *testing.T) {
	w := newEventWorld(t)
	ctx := context.Background()
	for _, repo := range []string{"one", "two", "three"} {
		w.record(model.EventStar, w.alice, w.aliceName, w.pubID, repo)
	}

	first, more, err := w.svc.FeedPage(ctx, int(w.alice), "yours", 1, 2)
	if err != nil || len(first) != 2 || !more {
		t.Fatalf("page 1 = %d events, more=%v, %v; want 2 and more", len(first), more, err)
	}
	second, more, err := w.svc.FeedPage(ctx, int(w.alice), "yours", 2, 2)
	if err != nil || len(second) != 1 || more {
		t.Fatalf("page 2 = %d events, more=%v, %v; want the 3rd event and no more", len(second), more, err)
	}
	if first[0].ID == second[0].ID || first[1].ID == second[0].ID {
		t.Error("an event appeared on two pages")
	}
}
