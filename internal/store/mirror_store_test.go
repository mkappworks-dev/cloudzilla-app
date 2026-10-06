package store_test

// Each test gets a fresh schema: ClaimDue sees every mirror in the database.

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type mirrorFixture struct {
	db     *sql.DB
	store  *store.MirrorStore
	userID int64
	owner  string
}

func newMirrorFixture(t *testing.T) mirrorFixture {
	t.Helper()
	db := testutil.OpenFreshTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	return mirrorFixture{db: db, store: store.NewMirrorStore(db), userID: testutil.SeedUser(t, db, suffix), owner: "testuser_" + suffix}
}

// seed creates a repo and its mirror, due at next.
func (f mirrorFixture) seed(t *testing.T, interval time.Duration, next time.Time) int64 {
	t.Helper()
	repoID := testutil.SeedRepo(t, f.db, f.userID, f.owner, testutil.UniqueSuffix(t))
	m := &model.RepoMirror{
		RepoID: repoID, RemoteURL: "https://example.com/up.git", AuthUsername: "bot",
		AuthTokenEnc: []byte{1, 2, 3}, Interval: interval, NextSyncAt: next, CreatedBy: f.userID,
	}
	if err := f.store.Create(context.Background(), m); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return repoID
}

func TestMirrorStore_CreateGetUpdateDelete(t *testing.T) {
	f := newMirrorFixture(t)
	ctx := context.Background()
	next := time.Now().Add(time.Hour).Truncate(time.Microsecond)
	repoID := f.seed(t, 8*time.Hour, next)

	m, err := f.store.Get(ctx, repoID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if m.RemoteURL != "https://example.com/up.git" || m.AuthUsername != "bot" || string(m.AuthTokenEnc) != "\x01\x02\x03" ||
		m.Interval != 8*time.Hour || !m.NextSyncAt.Equal(next) || m.CreatedBy != f.userID || m.OwnerName != f.owner || m.RepoName == "" {
		t.Errorf("Get = %+v", m)
	}

	m.RemoteURL, m.AuthTokenEnc, m.Interval = "https://example.com/other.git", nil, time.Hour
	if err := f.store.Update(ctx, m); err != nil {
		t.Fatalf("Update: %v", err)
	}
	m, _ = f.store.Get(ctx, repoID)
	if m.RemoteURL != "https://example.com/other.git" || m.AuthTokenEnc != nil || m.Interval != time.Hour {
		t.Errorf("after Update = %+v", m)
	}

	if err := f.store.Delete(ctx, repoID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := f.store.Get(ctx, repoID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Get after Delete err = %v, want sql.ErrNoRows", err)
	}
}

func TestMirrorStore_ClaimDue_SkipsNotDueLeasedArchivedAndDeleted(t *testing.T) {
	f := newMirrorFixture(t)
	ctx := context.Background()
	past := time.Now().Add(-time.Minute)
	due := f.seed(t, time.Hour, past)
	f.seed(t, time.Hour, time.Now().Add(time.Hour))
	leased := f.seed(t, time.Hour, past)
	testutil.Exec(t, f.db, `UPDATE repo_mirrors SET lease_until = NOW() + interval '1 hour' WHERE repo_id = $1`, leased)
	archived := f.seed(t, time.Hour, past)
	testutil.Exec(t, f.db, `UPDATE repositories SET is_archived = true WHERE id = $1`, archived)
	deleted := f.seed(t, time.Hour, past)
	testutil.Exec(t, f.db, `UPDATE repositories SET deleted_at = NOW() WHERE id = $1`, deleted)

	got, err := f.store.ClaimDue(ctx, 10, time.Minute)
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(got) != 1 || got[0].RepoID != due || got[0].OwnerName != f.owner {
		t.Fatalf("ClaimDue = %+v, want only repo %d", got, due)
	}
	if got[0].LeaseUntil == nil || !got[0].LeaseUntil.After(time.Now()) {
		t.Errorf("lease_until = %v, want in the future", got[0].LeaseUntil)
	}
	if again, _ := f.store.ClaimDue(ctx, 10, time.Minute); len(again) != 0 {
		t.Errorf("second ClaimDue = %+v, want nothing while the lease holds", again)
	}
}

func TestMirrorStore_ClaimDue_ExpiredLeaseIsReclaimed(t *testing.T) {
	f := newMirrorFixture(t)
	repoID := f.seed(t, time.Hour, time.Now().Add(-time.Minute))
	testutil.Exec(t, f.db, `UPDATE repo_mirrors SET lease_until = NOW() - interval '1 second' WHERE repo_id = $1`, repoID)

	got, err := f.store.ClaimDue(context.Background(), 10, time.Minute)
	if err != nil || len(got) != 1 {
		t.Fatalf("ClaimDue = %+v, %v; want the expired lease reclaimed", got, err)
	}
}

func TestMirrorStore_ClaimDue_ConcurrentClaimsNeverOverlap(t *testing.T) {
	f := newMirrorFixture(t)
	const mirrors, claimers = 20, 8
	for range mirrors {
		f.seed(t, time.Hour, time.Now().Add(-time.Minute))
	}

	var mu sync.Mutex
	seen := map[int64]int{}
	var wg sync.WaitGroup
	for range claimers {
		wg.Go(func() {
			got, err := f.store.ClaimDue(context.Background(), 5, time.Minute)
			if err != nil {
				t.Errorf("ClaimDue: %v", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, m := range got {
				seen[m.RepoID]++
			}
		})
	}
	wg.Wait()

	if len(seen) != mirrors {
		t.Errorf("claimed %d distinct mirrors, want %d", len(seen), mirrors)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("mirror %d claimed %d times", id, n)
		}
	}
}

func TestMirrorStore_RecordSuccessAndFailure(t *testing.T) {
	f := newMirrorFixture(t)
	ctx := context.Background()
	const interval = time.Hour
	repoID := f.seed(t, interval, time.Now().Add(-time.Minute))
	if _, err := f.store.ClaimDue(ctx, 1, time.Minute); err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}

	// The first failure keeps the interval; each further one doubles it, up to 24h.
	for i, wantGap := range []time.Duration{interval, 2 * interval, 4 * interval, 8 * interval, 16 * interval, 24 * time.Hour, 24 * time.Hour} {
		if err := f.store.RecordFailure(ctx, repoID, "upstream said no"); err != nil {
			t.Fatalf("RecordFailure: %v", err)
		}
		m, _ := f.store.Get(ctx, repoID)
		if m.ConsecutiveFailures != i+1 || m.LastError != "upstream said no" || m.LeaseUntil != nil || m.LastSyncAt == nil || m.LastSuccessAt != nil {
			t.Fatalf("after failure %d: %+v", i+1, m)
		}
		if gap := m.NextSyncAt.Sub(*m.LastSyncAt); gap != wantGap {
			t.Errorf("failure %d: next sync %v after the last, want %v", i+1, gap, wantGap)
		}
	}

	if err := f.store.RecordSuccess(ctx, repoID); err != nil {
		t.Fatalf("RecordSuccess: %v", err)
	}
	m, _ := f.store.Get(ctx, repoID)
	if m.ConsecutiveFailures != 0 || m.LastError != "" || m.LastSuccessAt == nil || m.LeaseUntil != nil {
		t.Errorf("after success: %+v", m)
	}
	if gap := m.NextSyncAt.Sub(*m.LastSuccessAt); gap != interval {
		t.Errorf("after success: next sync %v after it, want %v", gap, interval)
	}
}

func TestMirrorStore_Backoff_NeverShorterThanInterval(t *testing.T) {
	f := newMirrorFixture(t)
	ctx := context.Background()
	const week = 7 * 24 * time.Hour
	repoID := f.seed(t, week, time.Now())

	if err := f.store.RecordFailure(ctx, repoID, "x"); err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}
	if err := f.store.RecordFailure(ctx, repoID, "x"); err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}
	m, _ := f.store.Get(ctx, repoID)
	if gap := m.NextSyncAt.Sub(*m.LastSyncAt); gap != week {
		t.Errorf("next sync %v after the last, want the %v interval", gap, week)
	}
}

func TestMirrorStore_MarkDue(t *testing.T) {
	f := newMirrorFixture(t)
	ctx := context.Background()
	repoID := f.seed(t, time.Hour, time.Now().Add(time.Hour))

	if err := f.store.MarkDue(ctx, repoID); err != nil {
		t.Fatalf("MarkDue: %v", err)
	}
	got, err := f.store.ClaimDue(ctx, 10, time.Minute)
	if err != nil || len(got) != 1 {
		t.Errorf("ClaimDue after MarkDue = %+v, %v; want the mirror", got, err)
	}
}

func TestRepoStore_IsMirror(t *testing.T) {
	f := newMirrorFixture(t)
	ctx := context.Background()
	mirrorID := f.seed(t, time.Hour, time.Now())
	plainID := testutil.SeedRepo(t, f.db, f.userID, f.owner, testutil.UniqueSuffix(t))
	repos := store.NewRepoStore(f.db)

	for _, id := range []int64{mirrorID, plainID} {
		r, err := repos.GetByID(ctx, id)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if r.IsMirror != (id == mirrorID) {
			t.Errorf("GetByID(%d).IsMirror = %v", id, r.IsMirror)
		}
		byName, err := repos.GetByOwnerName(ctx, r.OwnerName, r.Name)
		if err != nil {
			t.Fatalf("GetByOwnerName: %v", err)
		}
		if byName.IsMirror != (id == mirrorID) {
			t.Errorf("GetByOwnerName(%s).IsMirror = %v", r.Name, byName.IsMirror)
		}
	}

	list, err := repos.GetByOwnerID(ctx, f.userID)
	if err != nil {
		t.Fatalf("GetByOwnerID: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("GetByOwnerID = %d repos, want 2", len(list))
	}
	for _, r := range list {
		if r.IsMirror != (r.ID == mirrorID) {
			t.Errorf("list: repo %d IsMirror = %v", r.ID, r.IsMirror)
		}
	}
}
