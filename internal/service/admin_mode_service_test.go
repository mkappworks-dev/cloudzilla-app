package service_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type adminModeFixture struct {
	svc    *service.AdminModeService
	db     *sql.DB
	userID int64
	now    time.Time
}

func newAdminModeFixture(t *testing.T) *adminModeFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	f := &adminModeFixture{db: db, userID: testutil.SeedSuperadmin(t, db, testutil.UniqueSuffix(t)), now: time.Now()}
	f.svc = service.NewAdminModeService(store.NewAdminModeStore(db), nil, time.Hour).WithClock(func() time.Time { return f.now })
	return f
}

func (f *adminModeFixture) enter(t *testing.T) string {
	t.Helper()
	raw, _, err := f.svc.Enter(context.Background(), nil, f.userID, "admin", 0)
	if err != nil {
		t.Fatalf("Enter: %v", err)
	}
	return raw
}

func (f *adminModeFixture) live(raw string) bool {
	_, ok := f.svc.Resolve(context.Background(), nil, raw, f.userID, "admin", 0)
	return ok
}

func TestAdminMode_EnterThenResolve(t *testing.T) {
	f := newAdminModeFixture(t)
	raw, expiresAt, err := f.svc.Enter(context.Background(), nil, f.userID, "admin", 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := f.now.Add(time.Hour); !expiresAt.Equal(want) {
		t.Errorf("expires %v, want %v", expiresAt, want)
	}
	if !f.live(raw) {
		t.Error("a fresh admin mode must resolve")
	}
}

func TestAdminMode_StoresOnlyTheTokenHash(t *testing.T) {
	f := newAdminModeFixture(t)
	raw := f.enter(t)
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM admin_modes WHERE token_hash = $1`, raw).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("the raw token must never be stored")
	}
}

func TestAdminMode_TokenIsBoundToItsUser(t *testing.T) {
	f := newAdminModeFixture(t)
	raw := f.enter(t)
	other := testutil.SeedSuperadmin(t, f.db, testutil.UniqueSuffix(t)+"b")
	if _, ok := f.svc.Resolve(context.Background(), nil, raw, other, "other", 0); ok {
		t.Error("another user must not resolve someone else's admin mode")
	}
}

func TestAdminMode_ExpiresAfterTheTTL(t *testing.T) {
	f := newAdminModeFixture(t)
	raw := f.enter(t)
	f.now = f.now.Add(time.Hour + time.Second)
	if f.live(raw) {
		t.Error("admin mode must end after its time box")
	}
}

func TestAdminMode_LeaveEndsItAtOnce(t *testing.T) {
	f := newAdminModeFixture(t)
	raw := f.enter(t)
	f.svc.Leave(context.Background(), nil, raw, f.userID, "admin")
	if f.live(raw) {
		t.Error("a left admin mode must not resolve")
	}
}

func TestAdminMode_EndsWhenSessionsAreRevoked(t *testing.T) {
	f := newAdminModeFixture(t)
	raw := f.enter(t)
	testutil.Exec(t, f.db, `UPDATE users SET session_version = session_version + 1 WHERE id = $1`, f.userID)
	if _, ok := f.svc.Resolve(context.Background(), nil, raw, f.userID, "admin", 1); ok {
		t.Error("a session-version bump (revoke, password change, suspension) must end admin mode, even for the new session")
	}
}

func TestAdminMode_EndsWhenTheRoleIsLost(t *testing.T) {
	f := newAdminModeFixture(t)
	raw := f.enter(t)
	testutil.Exec(t, f.db, `UPDATE users SET is_superadmin = false WHERE id = $1`, f.userID)
	if f.live(raw) {
		t.Error("a demoted user must lose admin mode")
	}
	testutil.Exec(t, f.db, `UPDATE users SET is_superadmin = true WHERE id = $1`, f.userID)
	if f.live(raw) {
		t.Error("re-promotion must not revive an admin mode that ended")
	}
}

func TestAdminMode_UnknownTokenDoesNotResolve(t *testing.T) {
	f := newAdminModeFixture(t)
	if f.live("nope") || f.live("") {
		t.Error("unknown tokens must not resolve")
	}
}
