package service_test

// Each test gets a fresh schema: the loop claims every due mirror in the database.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func (e mirrorEnv) addMirror(t *testing.T, repoID int64, url string, next time.Time) {
	t.Helper()
	m := &model.RepoMirror{RepoID: repoID, RemoteURL: url, Interval: time.Hour, NextSyncAt: next, CreatedBy: e.ownerID}
	if err := store.NewMirrorStore(e.db).Create(context.Background(), m); err != nil {
		t.Fatalf("create mirror: %v", err)
	}
}

func (e mirrorEnv) getMirror(t *testing.T, repoID int64) *model.RepoMirror {
	t.Helper()
	m, err := store.NewMirrorStore(e.db).Get(context.Background(), repoID)
	if err != nil {
		t.Fatalf("get mirror: %v", err)
	}
	return m
}

// runLoop runs the scheduler until the test ends, then checks it stopped.
func (e mirrorEnv) runLoop(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.svc.Mirror.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Run did not return after its context was cancelled")
		}
	})
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestMirrorRun_SyncsDueMirrorsOnly(t *testing.T) {
	e := newMirrorEnvOn(t, testutil.OpenFreshTestDB(t), true, 1)
	url := testutil.ServeGitHTTP(t, testutil.SeedSourceRepo(t).Dir, "", "")
	e.addMirror(t, e.repoID, url, time.Now().Add(-time.Minute))
	later, _, _ := e.addRepo(t)
	e.addMirror(t, later, url, time.Now().Add(time.Hour))

	e.runLoop(t)

	waitFor(t, "the due mirror to sync", func() bool { return e.getMirror(t, e.repoID).LastSuccessAt != nil })
	m := e.getMirror(t, e.repoID)
	if gap := m.NextSyncAt.Sub(*m.LastSuccessAt); gap != time.Hour {
		t.Errorf("next sync %v after the last, want the 1h interval", gap)
	}
	if m.LeaseUntil != nil {
		t.Errorf("lease_until = %v after the sync, want it released", m.LeaseUntil)
	}
	if got := refs(t, e.git)["refs/heads/main"]; got == "" {
		t.Error("the due mirror's main branch wasn't fetched")
	}
	if l := e.getMirror(t, later); l.LastSyncAt != nil {
		t.Errorf("the mirror not yet due synced at %v", l.LastSyncAt)
	}
}

func TestMirrorRun_RecordsFailure(t *testing.T) {
	e := newMirrorEnvOn(t, testutil.OpenFreshTestDB(t), true, 1)
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	e.addMirror(t, e.repoID, gone.URL+"/source.git", time.Now().Add(-time.Minute))

	e.runLoop(t)

	waitFor(t, "the failure to be recorded", func() bool { return e.getMirror(t, e.repoID).ConsecutiveFailures == 1 })
	if m := e.getMirror(t, e.repoID); m.LastError == "" || m.LastSuccessAt != nil || m.LeaseUntil != nil {
		t.Errorf("after a failed sync: %+v", m)
	}
}

func TestMirrorRun_NeverExceedsMaxConcurrent(t *testing.T) {
	const maxConcurrent, mirrors = 2, 5
	e := newMirrorEnvOn(t, testutil.OpenFreshTestDB(t), true, maxConcurrent)
	src := testutil.GitHTTPHandler(t, testutil.SeedSourceRepo(t).Dir, "", "")
	var inFlight, peak atomic.Int32
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
		}
		time.Sleep(100 * time.Millisecond)
		src.ServeHTTP(w, r)
	}))
	t.Cleanup(slow.Close)
	ids := []int64{e.repoID}
	for range mirrors - 1 {
		id, _, _ := e.addRepo(t)
		ids = append(ids, id)
	}
	for _, id := range ids {
		e.addMirror(t, id, slow.URL+"/source.git", time.Now().Add(-time.Minute))
	}

	e.runLoop(t)

	waitFor(t, "every mirror to sync", func() bool {
		for _, id := range ids {
			if e.getMirror(t, id).LastSuccessAt == nil {
				return false
			}
		}
		return true
	})
	if p := peak.Load(); p != maxConcurrent {
		t.Errorf("peak concurrent upstream requests = %d, want %d", p, maxConcurrent)
	}
}

func TestMirrorSyncNow_WakesTheLoop(t *testing.T) {
	e := newMirrorEnvOn(t, testutil.OpenFreshTestDB(t), true, 1)
	url := testutil.ServeGitHTTP(t, testutil.SeedSourceRepo(t).Dir, "", "")
	e.addMirror(t, e.repoID, url, time.Now().Add(time.Hour))
	e.runLoop(t)

	if err := e.svc.Mirror.SyncNow(context.Background(), e.repoID); err != nil {
		t.Fatalf("SyncNow: %v", err)
	}

	// Far sooner than the 30s poll: only the wake-up explains it.
	deadline := time.Now().Add(5 * time.Second)
	for e.getMirror(t, e.repoID).LastSuccessAt == nil {
		if time.Now().After(deadline) {
			t.Fatal("SyncNow didn't sync within 5s")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
