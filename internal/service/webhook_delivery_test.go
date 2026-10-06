package service_test

// Integration tests for webhook delivery. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type webhookFixture struct {
	store  *store.WebhookStore
	repoID int64
	srv    *httptest.Server
	hits   *atomic.Int32
}

func newWebhookFixture(t *testing.T, status int) webhookFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return webhookFixture{store: store.NewWebhookStore(db), repoID: repoID, srv: srv, hits: &hits}
}

// pendingDelivery stores, past the create-time check, a webhook on the
// fixture's loopback server with one failed delivery that is due for retry.
func (f webhookFixture) pendingDelivery(t *testing.T) model.WebhookDelivery {
	t.Helper()
	ctx := context.Background()
	wh := &model.Webhook{RepoID: f.repoID, URL: f.srv.URL + "/hook", Events: "push", Active: true}
	if err := f.store.Create(ctx, wh); err != nil {
		t.Fatalf("create webhook: %v", err)
	}
	d := &model.WebhookDelivery{WebhookID: wh.ID, Event: "push", Payload: `{}`, ResponseCode: 500}
	if err := f.store.LogDelivery(ctx, d); err != nil {
		t.Fatalf("log delivery: %v", err)
	}
	due := time.Now().Add(-time.Minute)
	if err := f.store.UpdateDeliveryRetry(ctx, d.ID, &due, 1, 500, ""); err != nil {
		t.Fatalf("schedule retry: %v", err)
	}
	return *d
}

func (f webhookFixture) waitForAttempt(t *testing.T, id int64, attempt int) *model.WebhookDelivery {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		d, err := f.store.GetDeliveryByID(context.Background(), id)
		if err != nil {
			t.Fatalf("get delivery: %v", err)
		}
		if d.AttemptCount >= attempt {
			return d
		}
		if time.Now().After(deadline) {
			t.Fatalf("delivery %d still at attempt %d", id, d.AttemptCount)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func assertRefused(t *testing.T, f webhookFixture, d *model.WebhookDelivery) {
	t.Helper()
	if d.Error != "127.0.0.1 resolves to a private network address" {
		t.Errorf("error = %q, want the private-network refusal", d.Error)
	}
	if d.NextRetryAt != nil {
		t.Errorf("a refused delivery was rescheduled for %v", d.NextRetryAt)
	}
	if f.hits.Load() != 0 {
		t.Errorf("private server was reached %d times", f.hits.Load())
	}
}

func TestWebhookRetryPending_RefusesPrivateTarget(t *testing.T) {
	f := newWebhookFixture(t, http.StatusOK)
	d := f.pendingDelivery(t)
	svc := service.NewWebhookService(f.store, config.WebhookConfig{})

	if err := svc.RetryPending(context.Background()); err != nil {
		t.Fatalf("RetryPending: %v", err)
	}
	got, err := f.store.GetDeliveryByID(context.Background(), d.ID)
	if err != nil {
		t.Fatalf("get delivery: %v", err)
	}
	if got.AttemptCount != 2 {
		t.Errorf("attempt_count = %d, want 2", got.AttemptCount)
	}
	assertRefused(t, f, got)
}

func TestWebhookRedeliver_RefusesPrivateTarget(t *testing.T) {
	f := newWebhookFixture(t, http.StatusOK)
	d := f.pendingDelivery(t)
	svc := service.NewWebhookService(f.store, config.WebhookConfig{})

	if err := svc.RedeliverByID(context.Background(), d.ID, f.repoID); err != nil {
		t.Fatalf("RedeliverByID: %v", err)
	}
	assertRefused(t, f, f.waitForAttempt(t, d.ID, 2))
}

func TestWebhookRetryPending_AllowLocalNetworks(t *testing.T) {
	f := newWebhookFixture(t, http.StatusOK)
	d := f.pendingDelivery(t)
	svc := service.NewWebhookService(f.store, config.WebhookConfig{AllowLocalNetworks: true})

	if err := svc.RetryPending(context.Background()); err != nil {
		t.Fatalf("RetryPending: %v", err)
	}
	got, err := f.store.GetDeliveryByID(context.Background(), d.ID)
	if err != nil {
		t.Fatalf("get delivery: %v", err)
	}
	if got.ResponseCode != http.StatusOK || got.Error != "" || got.NextRetryAt != nil {
		t.Errorf("delivery = code %d, error %q, next retry %v; want a cleared 200", got.ResponseCode, got.Error, got.NextRetryAt)
	}
	if f.hits.Load() != 1 {
		t.Errorf("server hits = %d, want 1", f.hits.Load())
	}
}

func TestWebhookDispatch_RecordsARedirectAndRetriesIt(t *testing.T) {
	f := newWebhookFixture(t, http.StatusFound)
	svc := service.NewWebhookService(f.store, config.WebhookConfig{AllowLocalNetworks: true})
	wh, err := svc.Create(context.Background(), f.repoID, f.srv.URL+"/hook", "", "push")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	svc.Dispatch(f.repoID, "push", map[string]any{"ref": "refs/heads/main"})
	deadline := time.Now().Add(5 * time.Second)
	for {
		ds, err := svc.ListDeliveries(context.Background(), wh.ID, f.repoID)
		if err != nil {
			t.Fatalf("ListDeliveries: %v", err)
		}
		if len(ds) == 1 && ds[0].NextRetryAt != nil {
			if ds[0].ResponseCode != http.StatusFound || ds[0].AttemptCount != 1 {
				t.Errorf("delivery = code %d attempt %d, want 302 attempt 1", ds[0].ResponseCode, ds[0].AttemptCount)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no scheduled retry recorded; deliveries = %+v", ds)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestWebhookCreate_RefusesPrivateURL(t *testing.T) {
	f := newWebhookFixture(t, http.StatusOK)
	svc := service.NewWebhookService(f.store, config.WebhookConfig{})
	_, err := svc.Create(context.Background(), f.repoID, f.srv.URL+"/hook", "", "push")
	var urlErr *service.WebhookURLError
	if !errors.As(err, &urlErr) {
		t.Fatalf("Create = %v, want WebhookURLError", err)
	}
	hooks, err := svc.ListByRepo(context.Background(), f.repoID)
	if err != nil {
		t.Fatalf("ListByRepo: %v", err)
	}
	if len(hooks) != 0 {
		t.Errorf("refused webhook was stored: %+v", hooks)
	}
}
