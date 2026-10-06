package service_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestMirrorStop_RefusedWhileASyncRuns(t *testing.T) {
	e := newMirrorEnvOn(t, testutil.OpenFreshTestDB(t), true, 1)
	ctx := context.Background()
	e.addMirror(t, e.repoID, "https://example.com/x.git", time.Now().Add(-time.Minute))
	if got, _ := store.NewMirrorStore(e.db).ClaimDue(ctx, 1, time.Hour); len(got) != 1 {
		t.Fatal("claim failed")
	}

	if err := e.svc.Mirror.Stop(ctx, e.repoID); !errors.Is(err, service.ErrMirrorSyncRunning) {
		t.Fatalf("Stop during a sync = %v, want ErrMirrorSyncRunning", err)
	}
	if err := store.NewMirrorStore(e.db).ReleaseLease(ctx, e.repoID); err != nil {
		t.Fatalf("ReleaseLease: %v", err)
	}
	if err := e.svc.Mirror.Stop(ctx, e.repoID); err != nil {
		t.Errorf("Stop once the sync ended: %v", err)
	}
}

func TestMirrorSync_UpstreamWithoutBranches_KeepsTheCopy(t *testing.T) {
	e := newMirrorEnv(t, true)
	ctx := context.Background()
	src := testutil.SeedSourceRepo(t)
	m := e.mirror(t, testutil.ServeGitHTTP(t, src.Dir, "", ""), nil)
	if err := e.svc.Mirror.Sync(ctx, m); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	upstream, _ := gogit.PlainOpen(src.Dir)
	for _, b := range []string{"main", "develop"} {
		if err := upstream.Storer.RemoveReference(plumbing.NewBranchReferenceName(b)); err != nil {
			t.Fatalf("remove %s: %v", b, err)
		}
	}
	before := refs(t, e.git)

	err := e.svc.Mirror.Sync(ctx, m)

	if err == nil || !strings.Contains(err.Error(), "no branches") {
		t.Errorf("Sync = %v, want a failure saying the source has no branches", err)
	}
	if after := refs(t, e.git); !mapsEqual(after, before) {
		t.Errorf("refs = %v, want the last copy %v", after, before)
	}
}

func TestMirrorShutdown_ReleasesTheLease(t *testing.T) {
	e := newMirrorEnvOn(t, testutil.OpenFreshTestDB(t), true, 1)
	stall := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-stall:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(stall); slow.Close() })
	e.addMirror(t, e.repoID, slow.URL+"/x.git", time.Now().Add(-time.Minute))
	ctx, cancel := context.WithCancel(context.Background())
	go e.svc.Mirror.Run(ctx)
	waitFor(t, "the sync to start", func() bool { return e.getMirror(t, e.repoID).LeaseUntil != nil })

	cancel()
	shutdownCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	e.svc.Mirror.Shutdown(shutdownCtx)

	m := e.getMirror(t, e.repoID)
	if m.LeaseUntil != nil || m.ConsecutiveFailures != 0 || m.LastError != "" || m.NextSyncAt.After(time.Now()) {
		t.Errorf("after shutdown: %+v; want the lease released, nothing recorded, due now", m)
	}
}

func TestMirrorUpdate_ReportsOnlyRealChanges(t *testing.T) {
	e := newMirrorEnv(t, true)
	m := e.mirror(t, "https://example.com/x.git", nil)
	same, user, hour := m.RemoteURL, "alice", time.Hour

	_, changed, err := e.svc.Mirror.Update(context.Background(), e.repoID, service.MirrorUpdate{RemoteURL: &same, AuthUsername: &user, Interval: &hour})
	if err != nil || len(changed) != 0 {
		t.Errorf("unchanged form: changed = %v, %v; want none", changed, err)
	}
	two := 2 * time.Hour
	_, changed, _ = e.svc.Mirror.Update(context.Background(), e.repoID, service.MirrorUpdate{RemoteURL: &same, Interval: &two})
	if strings.Join(changed, ",") != "interval" {
		t.Errorf("changed = %v, want [interval]", changed)
	}
}
