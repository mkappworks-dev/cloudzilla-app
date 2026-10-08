package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func seedWebhookRepo(t *testing.T, db *sql.DB) (repoID int64, suffix string) {
	t.Helper()
	suffix = testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	return testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix), suffix
}

func createWebhook(t *testing.T, s *store.WebhookStore, repoID int64, url string) *model.Webhook {
	t.Helper()
	wh := &model.Webhook{RepoID: repoID, URL: url, Secret: "s3cret", Events: "push", Active: true}
	if err := s.Create(context.Background(), wh); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return wh
}

func TestWebhookStore_CreateGetUpdateDelete(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewWebhookStore(db)
	repoID, _ := seedWebhookRepo(t, db)

	wh := createWebhook(t, s, repoID, "https://example.test/hook")
	if wh.ID == 0 {
		t.Fatal("Create must assign an ID")
	}
	got, err := s.GetByID(ctx, wh.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.RepoID != repoID || got.URL != wh.URL || got.Secret != "s3cret" || got.Events != "push" || !got.Active {
		t.Errorf("GetByID = %+v", got)
	}

	wh.URL, wh.Secret, wh.Events, wh.Active = "https://example.test/other", "", "issues", false
	if err := s.Update(ctx, wh); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err = s.GetByID(ctx, wh.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != wh.URL || got.Secret != "" || got.Events != "issues" || got.Active {
		t.Errorf("after Update = %+v", got)
	}
	if !got.UpdatedAt.After(got.CreatedAt) {
		t.Errorf("Update must advance updated_at: created %v, updated %v", got.CreatedAt, got.UpdatedAt)
	}

	if err := s.Delete(ctx, wh.ID, repoID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.GetByID(ctx, wh.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetByID after Delete = %v; want sql.ErrNoRows", err)
	}
}

func TestWebhookStore_GetByID_Unknown(t *testing.T) {
	db := testutil.OpenTestDB(t)
	if _, err := store.NewWebhookStore(db).GetByID(context.Background(), -1); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetByID = %v; want sql.ErrNoRows", err)
	}
}

func TestWebhookStore_Create_UnknownRepo(t *testing.T) {
	db := testutil.OpenTestDB(t)
	err := store.NewWebhookStore(db).Create(context.Background(), &model.Webhook{RepoID: -1, URL: "https://example.test"})
	if err == nil {
		t.Error("Create for a missing repo must fail the foreign key")
	}
}

func TestWebhookStore_DeleteAndUpdateEvents_AreScopedToTheRepo(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewWebhookStore(db)
	repoID, _ := seedWebhookRepo(t, db)
	otherRepoID, _ := seedWebhookRepo(t, db)
	wh := createWebhook(t, s, repoID, "https://example.test/a")

	if err := s.UpdateWebhookEvents(ctx, wh.ID, otherRepoID, "release"); err != nil {
		t.Fatalf("UpdateWebhookEvents: %v", err)
	}
	if got, _ := s.GetByID(ctx, wh.ID); got.Events != "push" {
		t.Errorf("events changed through another repo: %q", got.Events)
	}
	if err := s.Delete(ctx, wh.ID, otherRepoID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.GetByID(ctx, wh.ID); err != nil {
		t.Errorf("webhook deleted through another repo: %v", err)
	}

	if err := s.UpdateWebhookEvents(ctx, wh.ID, repoID, "release,push"); err != nil {
		t.Fatalf("UpdateWebhookEvents: %v", err)
	}
	if got, _ := s.GetByID(ctx, wh.ID); got.Events != "release,push" {
		t.Errorf("events = %q; want release,push", got.Events)
	}
}

func TestWebhookStore_ListByRepo(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewWebhookStore(db)
	repoID, _ := seedWebhookRepo(t, db)
	otherRepoID, _ := seedWebhookRepo(t, db)

	if hooks, err := s.ListByRepo(ctx, repoID); err != nil || len(hooks) != 0 {
		t.Fatalf("empty repo: ListByRepo = %v, %v", hooks, err)
	}
	first := createWebhook(t, s, repoID, "https://example.test/first")
	second := createWebhook(t, s, repoID, "https://example.test/second")
	createWebhook(t, s, otherRepoID, "https://example.test/other")

	hooks, err := s.ListByRepo(ctx, repoID)
	if err != nil {
		t.Fatal(err)
	}
	if len(hooks) != 2 || hooks[0].ID != second.ID || hooks[1].ID != first.ID {
		t.Errorf("ListByRepo = %+v; want newest first [%d %d]", hooks, second.ID, first.ID)
	}
}

func TestWebhookStore_DeleteCascadesToDeliveries(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewWebhookStore(db)
	repoID, _ := seedWebhookRepo(t, db)
	wh := createWebhook(t, s, repoID, "https://example.test/a")
	d := &model.WebhookDelivery{WebhookID: wh.ID, Event: "push", Payload: "{}"}
	if err := s.LogDelivery(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, wh.ID, repoID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetDeliveryByID(ctx, d.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetDeliveryByID after webhook delete = %v; want sql.ErrNoRows", err)
	}
}

func TestWebhookStore_LogDelivery_RoundTrip(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewWebhookStore(db)
	repoID, _ := seedWebhookRepo(t, db)
	wh := createWebhook(t, s, repoID, "https://example.test/a")

	d := &model.WebhookDelivery{WebhookID: wh.ID, Event: "push", Payload: `{"a":1}`, ResponseCode: 502, ResponseBody: "bad gateway", Error: "boom"}
	if err := s.LogDelivery(ctx, d); err != nil {
		t.Fatalf("LogDelivery: %v", err)
	}
	if d.ID == 0 {
		t.Fatal("LogDelivery must assign an ID")
	}
	got, err := s.GetDeliveryByID(ctx, d.ID)
	if err != nil {
		t.Fatalf("GetDeliveryByID: %v", err)
	}
	if got.WebhookID != wh.ID || got.Event != "push" || got.Payload != `{"a":1}` || got.ResponseCode != 502 ||
		got.ResponseBody != "bad gateway" || got.Error != "boom" || got.AttemptCount != 1 || got.NextRetryAt != nil {
		t.Errorf("GetDeliveryByID = %+v", got)
	}

	if err := s.LogDelivery(ctx, &model.WebhookDelivery{WebhookID: -1, Event: "push", Payload: "{}"}); err == nil {
		t.Error("LogDelivery for a missing webhook must fail")
	}
	if _, err := s.GetDeliveryByID(ctx, -1); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetDeliveryByID unknown = %v; want sql.ErrNoRows", err)
	}
}

func TestWebhookStore_ListDeliveries_NewestFirstCappedAt50(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewWebhookStore(db)
	repoID, _ := seedWebhookRepo(t, db)
	wh := createWebhook(t, s, repoID, "https://example.test/a")
	otherWH := createWebhook(t, s, repoID, "https://example.test/b")

	testutil.Exec(t, db,
		`INSERT INTO webhook_deliveries (webhook_id, event, payload, response_code, delivered_at)
		 SELECT $1, 'e' || g, '{}', 200, NOW() - make_interval(secs => g) FROM generate_series(1, 55) g`, wh.ID)
	testutil.Exec(t, db, `INSERT INTO webhook_deliveries (webhook_id, event, payload, response_code) VALUES ($1, 'other', '{}', 200)`, otherWH.ID)

	for name, list := range map[string]func(context.Context, int64) ([]model.WebhookDelivery, error){
		"ListDeliveries":          s.ListDeliveries,
		"ListDeliveriesWithRetry": s.ListDeliveriesWithRetry,
	} {
		t.Run(name, func(t *testing.T) {
			ds, err := list(ctx, wh.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(ds) != 50 {
				t.Fatalf("len = %d; want 50", len(ds))
			}
			if ds[0].Event != "e1" || ds[49].Event != "e50" {
				t.Errorf("order: first %q last %q; want e1 and e50", ds[0].Event, ds[49].Event)
			}
		})
	}

	if ds, err := s.ListDeliveries(ctx, -1); err != nil || len(ds) != 0 {
		t.Errorf("unknown webhook: %v, %v", ds, err)
	}
}

func TestWebhookStore_Retry(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewWebhookStore(db)
	repoID, suffix := seedWebhookRepo(t, db)
	wh := createWebhook(t, s, repoID, "https://example.test/a")

	log := func(event string) *model.WebhookDelivery {
		d := &model.WebhookDelivery{WebhookID: wh.ID, Event: event + suffix, Payload: "{}"}
		if err := s.LogDelivery(ctx, d); err != nil {
			t.Fatal(err)
		}
		return d
	}
	past := func(d time.Duration) *time.Time { v := time.Now().Add(-d); return &v }
	future := time.Now().Add(time.Hour)

	due := log("due")
	dueEarlier := log("earlier")
	notDue := log("notdue")
	exhausted := log("exhausted")
	cleared := log("cleared")
	for _, step := range []struct {
		d       *model.WebhookDelivery
		next    *time.Time
		attempt int
	}{
		{due, past(time.Minute), 2},
		{dueEarlier, past(time.Hour), 4},
		{notDue, &future, 2},
		{exhausted, past(time.Minute), 5},
		{cleared, nil, 3},
	} {
		if err := s.UpdateDeliveryRetry(ctx, step.d.ID, step.next, step.attempt, 500, "failed"); err != nil {
			t.Fatalf("UpdateDeliveryRetry: %v", err)
		}
	}

	got, err := s.GetDeliveryByID(ctx, due.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AttemptCount != 2 || got.ResponseCode != 500 || got.Error != "failed" || got.NextRetryAt == nil {
		t.Errorf("after UpdateDeliveryRetry = %+v", got)
	}
	if got, _ := s.GetDeliveryByID(ctx, cleared.ID); got.NextRetryAt != nil {
		t.Errorf("nil nextRetryAt must clear the retry, got %v", got.NextRetryAt)
	}

	pending, err := s.ListPendingRetry(ctx)
	if err != nil {
		t.Fatalf("ListPendingRetry: %v", err)
	}
	var ours []int64
	for _, d := range pending {
		if d.WebhookID == wh.ID {
			ours = append(ours, d.ID)
			if d.NextRetryAt == nil {
				t.Errorf("pending delivery %d has no next_retry_at", d.ID)
			}
		}
	}
	if len(ours) != 2 || ours[0] != dueEarlier.ID || ours[1] != due.ID {
		t.Errorf("pending ids = %v; want [%d %d] (earliest first, excluding not-due, exhausted and cleared)", ours, dueEarlier.ID, due.ID)
	}

	withRetry, err := s.ListDeliveriesWithRetry(ctx, wh.ID)
	if err != nil {
		t.Fatal(err)
	}
	byEvent := map[string]model.WebhookDelivery{}
	for _, d := range withRetry {
		byEvent[d.Event] = d
	}
	if d := byEvent["notdue"+suffix]; d.NextRetryAt == nil || !d.NextRetryAt.After(time.Now()) || d.AttemptCount != 2 {
		t.Errorf("notdue delivery = %+v", d)
	}
	if d := byEvent["cleared"+suffix]; d.NextRetryAt != nil || d.AttemptCount != 3 {
		t.Errorf("cleared delivery = %+v", d)
	}
}

func TestWebhookStore_ClosedDBErrors(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s := store.NewWebhookStore(db)

	checks := map[string]error{}
	checks["Create"] = s.Create(ctx, &model.Webhook{RepoID: 1})
	_, checks["ListByRepo"] = s.ListByRepo(ctx, 1)
	_, checks["GetByID"] = s.GetByID(ctx, 1)
	checks["Delete"] = s.Delete(ctx, 1, 1)
	checks["Update"] = s.Update(ctx, &model.Webhook{ID: 1})
	checks["LogDelivery"] = s.LogDelivery(ctx, &model.WebhookDelivery{WebhookID: 1})
	_, checks["ListDeliveries"] = s.ListDeliveries(ctx, 1)
	_, checks["ListPendingRetry"] = s.ListPendingRetry(ctx)
	checks["UpdateDeliveryRetry"] = s.UpdateDeliveryRetry(ctx, 1, nil, 1, 0, "")
	checks["UpdateWebhookEvents"] = s.UpdateWebhookEvents(ctx, 1, 1, "push")
	_, checks["ListDeliveriesWithRetry"] = s.ListDeliveriesWithRetry(ctx, 1)
	_, checks["GetDeliveryByID"] = s.GetDeliveryByID(ctx, 1)
	for name, err := range checks {
		if err == nil {
			t.Errorf("%s on a closed db must fail", name)
		}
	}
}
