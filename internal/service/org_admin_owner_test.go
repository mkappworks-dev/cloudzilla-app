package service_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestOrgService_AdminAddOwner(t *testing.T) {
	svc, db, ownerID := newOrgSvc(t)
	ctx := context.Background()
	sfx := testutil.UniqueSuffix(t)
	org, err := svc.Create(ctx, ownerID, "testorg_"+sfx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	testutil.DeleteOrgOnCleanup(t, db, org.ID)
	admin := testutil.SeedSuperadmin(t, db, sfx)
	stranger := testutil.SeedUser(t, db, "stranger_"+sfx)
	member := testutil.SeedUser(t, db, "member_"+sfx)
	testutil.Exec(t, db, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'member')`, org.ID, member)
	suspended := testutil.SeedUser(t, db, "suspended_"+sfx)
	testutil.Exec(t, db, `UPDATE users SET suspended_at = NOW() WHERE id = $1`, suspended)
	name := func(id int64) string {
		var n string
		if err := db.QueryRow(`SELECT username FROM users WHERE id = $1`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	for _, target := range []int64{stranger, member, ownerID} {
		if _, _, err := svc.AdminAddOwner(ctx, org.Name, name(target)); err != nil {
			t.Fatalf("AdminAddOwner(%d): %v", target, err)
		}
		if !svc.IsOwner(ctx, org.ID, target) {
			t.Errorf("user %d should own the org", target)
		}
	}
	if svc.IsMember(ctx, org.ID, admin) {
		t.Error("the superadmin must not become a member")
	}

	if _, _, err := svc.AdminAddOwner(ctx, org.Name, name(suspended)); !errors.Is(err, service.ErrUserSuspended) {
		t.Errorf("suspended target: got %v", err)
	}
	if _, _, err := svc.AdminAddOwner(ctx, org.Name, "nobody_"+sfx); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("unknown user: got %v", err)
	}
	if _, _, err := svc.AdminAddOwner(ctx, "noorg_"+sfx, name(stranger)); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("unknown org: got %v", err)
	}
	var ghost string
	if err := db.QueryRow(`SELECT username FROM users WHERE id = ghost_user_id()`).Scan(&ghost); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.AdminAddOwner(ctx, org.Name, ghost); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("ghost: got %v", err)
	}
}
