package store_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newGrant(t *testing.T, db *sql.DB, now time.Time) *model.DeviceGrant {
	t.Helper()
	suffix := testutil.UniqueSuffix(t)
	g := &model.DeviceGrant{
		DeviceCodeHash: "dh_" + suffix, UserCode: "UC" + suffix, Scopes: []string{"repo:write"},
		DeviceName: "laptop", RequesterIP: "ip_" + suffix, IntervalSecs: 5, ExpiresAt: now.Add(15 * time.Minute),
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM device_grants WHERE requester_ip = $1`, g.RequesterIP) })
	return g
}

func TestDeviceGrantStore_CreateCapsLiveGrantsPerIP(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx, now := context.Background(), time.Now()
	s := store.NewDeviceGrantStore(db)
	first := newGrant(t, db, now)
	for i := 0; i < 2; i++ {
		g := *first
		g.DeviceCodeHash, g.UserCode = first.DeviceCodeHash+string(rune('a'+i)), first.UserCode+string(rune('a'+i))
		if err := s.Create(ctx, &g, 2, now); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}
	g := *first
	g.DeviceCodeHash, g.UserCode = first.DeviceCodeHash+"z", first.UserCode+"z"
	if err := s.Create(ctx, &g, 2, now); !errors.Is(err, store.ErrTooManyGrants) {
		t.Fatalf("third Create = %v; want ErrTooManyGrants", err)
	}
}

func TestDeviceGrantStore_ApproveOnlyWhilePending(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx, now := context.Background(), time.Now()
	s := store.NewDeviceGrantStore(db)
	uid := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	g := newGrant(t, db, now)
	if err := s.Create(ctx, g, 5, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(ctx, g.UserCode, uid, []string{"repo:read"}, now); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if err := s.Approve(ctx, g.UserCode, uid, []string{"repo:read"}, now); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("second Approve = %v; want sql.ErrNoRows", err)
	}
	if err := s.Deny(ctx, g.UserCode, uid, now); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Deny after Approve = %v; want sql.ErrNoRows", err)
	}
	got, err := s.GetByDeviceHash(ctx, g.DeviceCodeHash)
	if err != nil || got.Status != model.DeviceGrantApproved || len(got.Scopes) != 1 || got.Scopes[0] != "repo:read" {
		t.Errorf("grant = %+v, %v; want approved with narrowed scopes", got, err)
	}
}

func TestDeviceGrantStore_ApproveRefusesExpired(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx, now := context.Background(), time.Now()
	s := store.NewDeviceGrantStore(db)
	uid := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	g := newGrant(t, db, now)
	if err := s.Create(ctx, g, 5, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(ctx, g.UserCode, uid, g.Scopes, now.Add(16*time.Minute)); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Approve after expiry = %v; want sql.ErrNoRows", err)
	}
}

func TestDeviceGrantStore_TouchFlagsEarlyPollsAndGrowsInterval(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx, now := context.Background(), time.Now()
	s := store.NewDeviceGrantStore(db)
	g := newGrant(t, db, now)
	if err := s.Create(ctx, g, 5, now); err != nil {
		t.Fatal(err)
	}
	if fast, err := s.Touch(ctx, g.ID, now); err != nil || fast {
		t.Fatalf("first Touch = %v, %v; want false", fast, err)
	}
	if fast, _ := s.Touch(ctx, g.ID, now.Add(2*time.Second)); !fast {
		t.Error("poll after 2s with a 5s interval should be too fast")
	}
	got, _ := s.GetByDeviceHash(ctx, g.DeviceCodeHash)
	if got.IntervalSecs != 10 {
		t.Errorf("interval = %d; want 10 after slow_down", got.IntervalSecs)
	}
	if fast, _ := s.Touch(ctx, g.ID, now.Add(13*time.Second)); fast {
		t.Error("poll 11s after the last one with a 10s interval should be fine")
	}
}

func TestDeviceGrantStore_RedeemOnce(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx, now := context.Background(), time.Now()
	s := store.NewDeviceGrantStore(db)
	uid := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	g := newGrant(t, db, now)
	if err := s.Create(ctx, g, 5, now); err != nil {
		t.Fatal(err)
	}
	noop := func(context.Context, *sql.Tx, *model.DeviceGrant) error { return nil }
	if err := s.Redeem(ctx, g.DeviceCodeHash, now, noop); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("Redeem of a pending grant = %v; want sql.ErrNoRows", err)
	}
	if err := s.Approve(ctx, g.UserCode, uid, g.Scopes, now); err != nil {
		t.Fatal(err)
	}
	var seen *model.DeviceGrant
	if err := s.Redeem(ctx, g.DeviceCodeHash, now, func(_ context.Context, _ *sql.Tx, got *model.DeviceGrant) error { seen = got; return nil }); err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	if seen == nil || seen.UserID.Int64 != uid {
		t.Errorf("callback grant = %+v; want user %d", seen, uid)
	}
	if err := s.Redeem(ctx, g.DeviceCodeHash, now, noop); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("second Redeem = %v; want sql.ErrNoRows", err)
	}
}

func TestDeviceGrantStore_RedeemRollsBackWhenInsertFails(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx, now := context.Background(), time.Now()
	s := store.NewDeviceGrantStore(db)
	uid := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	g := newGrant(t, db, now)
	_ = s.Create(ctx, g, 5, now)
	_ = s.Approve(ctx, g.UserCode, uid, g.Scopes, now)
	boom := errors.New("boom")
	if err := s.Redeem(ctx, g.DeviceCodeHash, now, func(context.Context, *sql.Tx, *model.DeviceGrant) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("Redeem = %v; want boom", err)
	}
	got, _ := s.GetByDeviceHash(ctx, g.DeviceCodeHash)
	if got.Status != model.DeviceGrantApproved {
		t.Errorf("status = %s; want approved so the next poll can retry", got.Status)
	}
}

func TestDeviceGrantStore_ConcurrentRedeemMintsOnce(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx, now := context.Background(), time.Now()
	s := store.NewDeviceGrantStore(db)
	uid := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	g := newGrant(t, db, now)
	if err := s.Create(ctx, g, 5, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(ctx, g.UserCode, uid, g.Scopes, now); err != nil {
		t.Fatal(err)
	}
	const workers = 8
	var inserts atomic.Int32
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = s.Redeem(ctx, g.DeviceCodeHash, now, func(context.Context, *sql.Tx, *model.DeviceGrant) error {
				inserts.Add(1)
				return nil
			})
		}()
	}
	wg.Wait()
	var ok, noRows int
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, sql.ErrNoRows):
			noRows++
		default:
			t.Errorf("unexpected Redeem error: %v", err)
		}
	}
	if ok != 1 || noRows != workers-1 || inserts.Load() != 1 {
		t.Errorf("ok=%d noRows=%d inserts=%d; want 1, %d, 1", ok, noRows, inserts.Load(), workers-1)
	}
}
