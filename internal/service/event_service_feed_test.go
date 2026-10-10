package service_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type eventWorld struct {
	svc                *service.EventService
	db                 *sql.DB
	alice, bob         int64
	aliceName, bobName string
	pubID, privID      int64
	pubName, privName  string
}

// newEventWorld has alice owning a public and a private repo and bob owning nothing.
func newEventWorld(t *testing.T) *eventWorld {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	w := &eventWorld{db: db}
	w.alice = testutil.SeedUser(t, db, "a"+suffix)
	w.bob = testutil.SeedUser(t, db, "b"+suffix)
	w.aliceName, w.bobName = "testuser_a"+suffix, "testuser_b"+suffix
	w.pubID = testutil.SeedRepo(t, db, w.alice, w.aliceName, "pub"+suffix)
	w.privID = testutil.SeedRepo(t, db, w.alice, w.aliceName, "priv"+suffix)
	w.pubName, w.privName = "testrepo_pub"+suffix, "testrepo_priv"+suffix
	testutil.Exec(t, db, `UPDATE repositories SET private = true WHERE id = $1`, w.privID)
	w.svc = service.NewEventService(store.NewEventStore(db), store.NewUserStore(db), store.NewRepoStore(db))
	return w
}

func (w *eventWorld) record(eventType string, actorID int64, actorName string, repoID int64, repoName string) {
	w.svc.Record(context.Background(), actorID, actorName, &repoID, repoName, w.aliceName, eventType, map[string]any{"n": 1})
}

func eventTypes(events []model.Event) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.EventType
	}
	return out
}

func TestEventService_RecordPush_StoresBranchAndCommits(t *testing.T) {
	w := newEventWorld(t)
	ctx := context.Background()

	w.svc.RecordPush(ctx, w.alice, w.aliceName, &w.pubID, w.pubName, w.aliceName,
		model.PushSummary{Branch: "main", CommitTotal: 3})

	events, err := w.svc.RepoActivity(ctx, w.aliceName, w.pubName, 1, 30)
	if err != nil || len(events) != 1 {
		t.Fatalf("RepoActivity = %d events, %v; want 1", len(events), err)
	}
	e := events[0]
	if e.EventType != model.EventPush || e.ActorID != w.alice || e.RepoID == nil || *e.RepoID != w.pubID {
		t.Errorf("event = %+v", e)
	}
	var payload struct {
		Branch      string `json:"branch"`
		CommitTotal int    `json:"commit_total"`
	}
	if err := json.Unmarshal(e.Payload, &payload); err != nil || payload.Branch != "main" || payload.CommitTotal != 3 {
		t.Errorf("payload = %s (%v)", e.Payload, err)
	}
}

func TestEventService_Record_StoresEmptyPayloadWhenItCannotBeMarshalled(t *testing.T) {
	w := newEventWorld(t)

	w.svc.Record(context.Background(), w.alice, w.aliceName, &w.pubID, w.pubName, w.aliceName,
		model.EventStar, map[string]any{"bad": make(chan int)})

	events, err := w.svc.RepoActivity(context.Background(), w.aliceName, w.pubName, 1, 30)
	if err != nil || len(events) != 1 {
		t.Fatalf("RepoActivity = %d events, %v; want 1", len(events), err)
	}
	if string(events[0].Payload) != "{}" {
		t.Errorf("payload = %s; want {}", events[0].Payload)
	}
}

func TestEventService_Record_SwallowsStoreErrors(t *testing.T) {
	w := newEventWorld(t)

	// No such actor: the foreign key rejects the row, and Record only logs it.
	w.svc.Record(context.Background(), 0, "ghost", &w.pubID, w.pubName, w.aliceName, model.EventStar, nil)

	events, err := w.svc.RepoActivity(context.Background(), w.aliceName, w.pubName, 1, 30)
	if err != nil || len(events) != 0 {
		t.Errorf("RepoActivity = %d events, %v; want none", len(events), err)
	}
}

func TestEventService_RepoActivity(t *testing.T) {
	w := newEventWorld(t)
	ctx := context.Background()
	w.record(model.EventIssueOpened, w.alice, w.aliceName, w.pubID, w.pubName)
	w.record(model.EventStar, w.bob, w.bobName, w.pubID, w.pubName)
	w.record(model.EventIssueOpened, w.alice, w.aliceName, w.privID, w.privName)

	t.Run("lists a public repo's events newest first", func(t *testing.T) {
		events, err := w.svc.RepoActivity(ctx, w.aliceName, w.pubName, 1, 30)
		if err != nil {
			t.Fatal(err)
		}
		if got := eventTypes(events); len(got) != 2 || got[0] != model.EventStar || got[1] != model.EventIssueOpened {
			t.Errorf("types = %v; want [star issue_opened]", got)
		}
	})

	t.Run("hides a private repo's events without an error", func(t *testing.T) {
		events, err := w.svc.RepoActivity(ctx, w.aliceName, w.privName, 1, 30)
		if err != nil || events == nil || len(events) != 0 {
			t.Errorf("events = %v, %v; want an empty, non-nil list", events, err)
		}
	})

	t.Run("reports an unknown repo", func(t *testing.T) {
		if _, err := w.svc.RepoActivity(ctx, w.aliceName, "no-such-repo", 1, 30); err == nil {
			t.Error("want an error")
		}
	})

	t.Run("pages, and falls back to page 1 and 30 rows for out-of-range values", func(t *testing.T) {
		second, err := w.svc.RepoActivity(ctx, w.aliceName, w.pubName, 2, 1)
		if err != nil || len(second) != 1 || second[0].EventType != model.EventIssueOpened {
			t.Errorf("page 2 of 1 = %v, %v; want the older event", eventTypes(second), err)
		}
		for _, c := range []struct{ page, size int }{{0, 0}, {-3, 101}} {
			all, err := w.svc.RepoActivity(ctx, w.aliceName, w.pubName, c.page, c.size)
			if err != nil || len(all) != 2 {
				t.Errorf("RepoActivity(page %d, size %d) = %d events, %v; want 2", c.page, c.size, len(all), err)
			}
		}
	})
}

func TestEventService_UserActivity(t *testing.T) {
	w := newEventWorld(t)
	ctx := context.Background()
	w.record(model.EventIssueOpened, w.alice, w.aliceName, w.pubID, w.pubName)
	w.record(model.EventIssueOpened, w.alice, w.aliceName, w.privID, w.privName)
	w.record(model.EventStar, w.bob, w.bobName, w.pubID, w.pubName)

	t.Run("lists the user's public events only", func(t *testing.T) {
		events, err := w.svc.UserActivity(ctx, w.aliceName, 1, 15)
		if err != nil || len(events) != 1 || events[0].RepoName != w.pubName {
			t.Errorf("events = %+v, %v; want only the public repo's event", events, err)
		}
	})

	t.Run("reports an unknown user", func(t *testing.T) {
		if _, err := w.svc.UserActivity(ctx, "no-such-user", 1, 15); err == nil {
			t.Error("want an error")
		}
	})

	t.Run("falls back to page 1 and 15 rows for out-of-range values", func(t *testing.T) {
		events, err := w.svc.UserActivity(ctx, w.aliceName, 0, 0)
		if err != nil || len(events) != 1 {
			t.Errorf("events = %d, %v; want 1", len(events), err)
		}
	})
}

func TestEventService_Feed(t *testing.T) {
	w := newEventWorld(t)
	ctx := context.Background()
	w.record(model.EventIssueOpened, w.alice, w.aliceName, w.pubID, w.pubName)
	w.record(model.EventIssueOpened, w.alice, w.aliceName, w.privID, w.privName)
	w.record(model.EventStar, w.bob, w.bobName, w.pubID, w.pubName)
	testutil.Exec(t, w.db, `INSERT INTO watches (user_id, repo_id) VALUES ($1, $2)`, w.bob, w.pubID)

	feed := func(userID int64, filter string) []model.Event {
		t.Helper()
		events, err := w.svc.Feed(ctx, int(userID), filter, 1, 30)
		if err != nil {
			t.Fatalf("Feed(%q): %v", filter, err)
		}
		return events
	}

	t.Run("yours is what the user did, private repos included", func(t *testing.T) {
		if got := len(feed(w.alice, "yours")); got != 2 {
			t.Errorf("alice yours = %d; want 2", got)
		}
		if got := len(feed(w.bob, "yours")); got != 1 {
			t.Errorf("bob yours = %d; want 1", got)
		}
	})

	t.Run("watching is the watched repos' events", func(t *testing.T) {
		got := feed(w.bob, "watching")
		if len(got) != 2 {
			t.Fatalf("bob watching = %v; want both public-repo events", eventTypes(got))
		}
		for _, e := range got {
			if *e.RepoID != w.pubID {
				t.Errorf("event from repo %d; want only the watched repo %d", *e.RepoID, w.pubID)
			}
		}
		if got := len(feed(w.alice, "watching")); got != 0 {
			t.Errorf("alice watching = %d; want 0", got)
		}
	})

	t.Run("all and the empty filter add the user's own repos", func(t *testing.T) {
		for _, f := range []string{"all", "", "bogus"} {
			if got := len(feed(w.alice, f)); got != 3 {
				t.Errorf("alice filter %q = %d; want 3 (every event in her repos)", f, got)
			}
		}
		if got := len(feed(w.bob, "all")); got != 2 {
			t.Errorf("bob all = %d; want 2 (the watched repo only, never alice's private one)", got)
		}
	})

	t.Run("falls back to page 1 and 30 rows for out-of-range values", func(t *testing.T) {
		events, err := w.svc.Feed(ctx, int(w.alice), "yours", 0, 0)
		if err != nil || len(events) != 2 {
			t.Errorf("events = %d, %v; want 2", len(events), err)
		}
		one, err := w.svc.Feed(ctx, int(w.alice), "yours", 2, 1)
		if err != nil || len(one) != 1 {
			t.Errorf("page 2 of 1 = %d, %v; want 1", len(one), err)
		}
	})
}

func TestEventService_FeedCounts(t *testing.T) {
	w := newEventWorld(t)
	w.record(model.EventIssueOpened, w.alice, w.aliceName, w.pubID, w.pubName)
	w.record(model.EventIssueOpened, w.alice, w.aliceName, w.privID, w.privName)
	w.record(model.EventStar, w.bob, w.bobName, w.pubID, w.pubName)
	testutil.Exec(t, w.db, `INSERT INTO watches (user_id, repo_id) VALUES ($1, $2)`, w.bob, w.pubID)

	got, err := w.svc.FeedCounts(context.Background(), int(w.bob))
	if err != nil {
		t.Fatal(err)
	}
	if got["all"] != 2 || got["yours"] != 1 || got["watching"] != 2 {
		t.Errorf("counts = %v; want all=2 yours=1 watching=2", got)
	}
}
