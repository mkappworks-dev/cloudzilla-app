package service_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func hmacHex(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

type receivedHook struct {
	event, signature string
	body             []byte
}

type hookReceiver struct {
	url string
	mu  sync.Mutex
	got []receivedHook
}

func newHookReceiver(t *testing.T, status int) *hookReceiver {
	t.Helper()
	r := &hookReceiver{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.got = append(r.got, receivedHook{req.Header.Get("X-Cloudzilla-Event"), req.Header.Get("X-Hub-Signature-256"), body})
		r.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	r.url = srv.URL
	return r
}

func (r *hookReceiver) events() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var ev []string
	for _, h := range r.got {
		ev = append(ev, h.event)
	}
	return ev
}

func (r *hookReceiver) waitFor(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(r.events()) >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("received %v; want %d deliveries", r.events(), n)
}

type orgHookFixture struct {
	db           *sql.DB
	svc          *service.WebhookService
	store        *store.WebhookStore
	orgID        int64
	repoA, repoB model.Repository
	other        model.Repository
}

func newOrgHookFixture(t *testing.T) orgHookFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	seedOrg := func(name string) (int64, model.Repository) {
		var orgID, repoID int64
		if err := db.QueryRow(`INSERT INTO organizations (name) VALUES ($1) RETURNING id`, name).Scan(&orgID); err != nil {
			t.Fatal(err)
		}
		testutil.DeleteOrgOnCleanup(t, db, orgID)
		if err := db.QueryRow(`INSERT INTO repositories (owner_name, org_id, name) VALUES ($1, $2, 'app') RETURNING id`, name, orgID).Scan(&repoID); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM repositories WHERE id = $1`, repoID) })
		return orgID, model.Repository{ID: repoID, Name: "app"}
	}
	orgID, repoA := seedOrg("orga_" + suffix)
	var repoBID int64
	if err := db.QueryRow(`INSERT INTO repositories (owner_name, org_id, name) VALUES ($1, $2, 'lib') RETURNING id`, "orga_"+suffix, orgID).Scan(&repoBID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM repositories WHERE id = $1`, repoBID) })
	_, other := seedOrg("orgb_" + suffix)

	s := store.NewWebhookStore(db)
	return orgHookFixture{
		db: db, store: s, orgID: orgID,
		svc:   service.NewWebhookService(s, config.WebhookConfig{AllowLocalNetworks: true}),
		repoA: repoA, repoB: model.Repository{ID: repoBID, Name: "lib"}, other: other,
	}
}

func TestWebhookDispatch_OrgHookReceivesEveryOrgRepo(t *testing.T) {
	f := newOrgHookFixture(t)
	rx := newHookReceiver(t, http.StatusOK)
	if _, err := f.svc.CreateForOrg(context.Background(), f.orgID, rx.url, "s3cret", ""); err != nil {
		t.Fatal(err)
	}

	f.svc.Dispatch(f.repoA.ID, "push", f.svc.PushPayload(f.repoA, "alice", "main", "abc"))
	f.svc.Dispatch(f.repoB.ID, "issues", f.svc.IssuePayload("opened", f.repoB, model.Issue{Number: 1, Title: "t"}))
	f.svc.Dispatch(f.repoB.ID, "pull_request", f.svc.PullPayload("opened", f.repoB, model.PullRequest{Number: 2}))
	// Neither another organization's repository nor an event the hook didn't ask for reaches it.
	f.svc.Dispatch(f.other.ID, "push", f.svc.PushPayload(f.other, "bob", "main", "def"))
	f.svc.Dispatch(f.repoA.ID, "release", map[string]any{})

	rx.waitFor(t, 3)
	time.Sleep(100 * time.Millisecond)
	if ev := rx.events(); len(ev) != 3 {
		t.Fatalf("events = %v; want push, issues and pull_request once each", ev)
	}
	rx.mu.Lock()
	defer rx.mu.Unlock()
	for _, h := range rx.got {
		if want := "sha256=" + hmacHex(h.body, "s3cret"); h.signature != want {
			t.Errorf("%s signature = %q; want %q", h.event, h.signature, want)
		}
	}
}

func TestWebhookDispatch_TransferredOutRepoStopsFiring(t *testing.T) {
	f := newOrgHookFixture(t)
	rx := newHookReceiver(t, http.StatusOK)
	if _, err := f.svc.CreateForOrg(context.Background(), f.orgID, rx.url, "", "push"); err != nil {
		t.Fatal(err)
	}
	userID := testutil.SeedUser(t, f.db, testutil.UniqueSuffix(t))

	f.svc.Dispatch(f.repoB.ID, "push", f.svc.PushPayload(f.repoB, "a", "main", "1"))
	rx.waitFor(t, 1)

	testutil.Exec(t, f.db, `UPDATE repositories SET org_id = NULL, owner_id = $2 WHERE id = $1`, f.repoB.ID, userID)
	f.svc.Dispatch(f.repoB.ID, "push", f.svc.PushPayload(f.repoB, "a", "main", "2"))
	time.Sleep(200 * time.Millisecond)
	if n := len(rx.events()); n != 1 {
		t.Errorf("deliveries after transfer out = %d; want still 1", n)
	}

	testutil.Exec(t, f.db, `UPDATE repositories SET owner_id = NULL, org_id = $2 WHERE id = $1`, f.repoB.ID, f.orgID)
	f.svc.Dispatch(f.repoB.ID, "push", f.svc.PushPayload(f.repoB, "a", "main", "3"))
	rx.waitFor(t, 2)
}

func TestWebhookDispatch_OrgHookFailureIsRetried(t *testing.T) {
	f := newOrgHookFixture(t)
	rx := newHookReceiver(t, http.StatusInternalServerError)
	wh, err := f.svc.CreateForOrg(context.Background(), f.orgID, rx.url, "", "push")
	if err != nil {
		t.Fatal(err)
	}

	f.svc.Dispatch(f.repoA.ID, "push", f.svc.PushPayload(f.repoA, "a", "main", "1"))
	rx.waitFor(t, 1)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ds, err := f.store.ListDeliveriesWithRetry(context.Background(), wh.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(ds) == 1 && ds[0].NextRetryAt != nil {
			if ds[0].ResponseCode != http.StatusInternalServerError {
				t.Errorf("response code = %d; want 500", ds[0].ResponseCode)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("a 500 from an org hook was not scheduled for retry")
}

func TestWebhookCreateForOrg_RefusesPrivateURL(t *testing.T) {
	f := newOrgHookFixture(t)
	svc := service.NewWebhookService(f.store, config.WebhookConfig{})
	_, err := svc.CreateForOrg(context.Background(), f.orgID, "http://127.0.0.1:9/hook", "", "push")
	var urlErr *service.WebhookURLError
	if !errors.As(err, &urlErr) {
		t.Fatalf("CreateForOrg = %v; want WebhookURLError", err)
	}
	if hooks, _ := svc.ListByOrg(context.Background(), f.orgID); len(hooks) != 0 {
		t.Errorf("refused webhook was stored: %+v", hooks)
	}
}

func TestWebhookOrgScoping(t *testing.T) {
	f := newOrgHookFixture(t)
	ctx := context.Background()
	wh, err := f.svc.CreateForOrg(ctx, f.orgID, "http://example.test/h", "", "push")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ListDeliveriesForOrg(ctx, wh.ID, f.orgID+1000000); err == nil || err.Error() != "forbidden" {
		t.Errorf("ListDeliveriesForOrg for another org = %v; want forbidden", err)
	}
	if _, err := f.svc.ListDeliveries(ctx, wh.ID, f.repoA.ID); err == nil || err.Error() != "forbidden" {
		t.Errorf("repo ListDeliveries on an org hook = %v; want forbidden", err)
	}
}
