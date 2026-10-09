package store_test

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type orgFixture struct {
	db     *sql.DB
	os     *store.OrgStore
	suffix string
}

func newOrgFixture(t *testing.T) orgFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	return orgFixture{db: db, os: store.NewOrgStore(db), suffix: testutil.UniqueSuffix(t)}
}

func (f orgFixture) org(t *testing.T, name string) *model.Organization {
	t.Helper()
	o := &model.Organization{Name: name + "_" + f.suffix, DisplayName: "Display " + name}
	if err := f.os.Create(context.Background(), o); err != nil {
		t.Fatalf("Create %s: %v", name, err)
	}
	testutil.DeleteOrgOnCleanup(t, f.db, o.ID)
	return o
}

func (f orgFixture) user(t *testing.T, tag string) int64 {
	t.Helper()
	return testutil.SeedUser(t, f.db, f.suffix+"_"+tag)
}

func (f orgFixture) member(t *testing.T, orgID, userID int64, role model.OrgRole) {
	t.Helper()
	if err := f.os.AddMember(context.Background(), orgID, userID, role); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
}

func (f orgFixture) orgRepo(t *testing.T, o *model.Organization, name string, softDeleted bool) int64 {
	t.Helper()
	var id int64
	err := f.db.QueryRowContext(context.Background(),
		`INSERT INTO repositories (owner_name, org_id, name, deleted_at)
		 VALUES ($1, $2, $3, CASE WHEN $4 THEN NOW() END) RETURNING id`,
		o.Name, o.ID, name, softDeleted,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seed org repo: %v", err)
	}
	t.Cleanup(func() { testutil.Exec(t, f.db, `DELETE FROM repositories WHERE id = $1`, id) })
	return id
}

func TestOrgStore_Create_NameTaken(t *testing.T) {
	f := newOrgFixture(t)
	ctx := context.Background()
	existing := f.org(t, "acme")
	userID := f.user(t, "u")
	var username string
	if err := f.db.QueryRowContext(ctx, `SELECT username FROM users WHERE id = $1`, userID).Scan(&username); err != nil {
		t.Fatalf("username: %v", err)
	}

	tests := []struct {
		name string
		org  string
	}{
		{"same org name", existing.Name},
		{"org name in other case", strings.ToUpper(existing.Name)},
		{"username", username},
		{"username in other case", strings.ToUpper(username)},
	}
	for _, tc := range tests {
		err := f.os.Create(ctx, &model.Organization{Name: tc.org})
		if !errors.Is(err, store.ErrOrgNameTaken) {
			t.Errorf("%s: err = %v, want ErrOrgNameTaken", tc.name, err)
		}
	}
}

func TestOrgStore_GetByID(t *testing.T) {
	f := newOrgFixture(t)
	ctx := context.Background()
	o := f.org(t, "byid")

	got, err := f.os.GetByID(ctx, o.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != o.Name || got.DisplayName != o.DisplayName {
		t.Errorf("got %+v", got)
	}
	if _, err := f.os.GetByID(ctx, o.ID+1_000_000); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("unknown id: %v, want sql.ErrNoRows", err)
	}
}

func TestOrgStore_UpdateRepoDefaults(t *testing.T) {
	f := newOrgFixture(t)
	ctx := context.Background()
	o := f.org(t, "defaults")

	if err := f.os.UpdateRepoDefaults(ctx, o.ID, "private", "trunk"); err != nil {
		t.Fatalf("UpdateRepoDefaults: %v", err)
	}
	got, _ := f.os.GetByID(ctx, o.ID)
	if got.DefaultRepoVisibility != "private" || got.DefaultBranchName != "trunk" {
		t.Errorf("defaults = %q/%q", got.DefaultRepoVisibility, got.DefaultBranchName)
	}
	if err := f.os.UpdateRepoDefaults(ctx, o.ID+1_000_000, "public", "main"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("unknown org: %v, want sql.ErrNoRows", err)
	}
}

func TestOrgStore_UpdateProfile(t *testing.T) {
	f := newOrgFixture(t)
	ctx := context.Background()
	o := f.org(t, "profile")

	if err := f.os.UpdateProfile(ctx, o.ID, "New Name", "about us", "https://example.test", "Nowhere", "hi@example.test"); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	got, _ := f.os.GetByID(ctx, o.ID)
	if got.DisplayName != "New Name" || got.Description != "about us" || got.Website != "https://example.test" ||
		got.Location != "Nowhere" || got.ContactEmail != "hi@example.test" {
		t.Errorf("got %+v", got)
	}
	if got.Name != o.Name {
		t.Errorf("Name changed to %q", got.Name)
	}
	if err := f.os.UpdateProfile(ctx, o.ID+1_000_000, "", "", "", "", ""); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("unknown org: %v, want sql.ErrNoRows", err)
	}
}

func TestOrgStore_AddMember_Duplicate(t *testing.T) {
	f := newOrgFixture(t)
	o := f.org(t, "dup")
	u := f.user(t, "u")
	f.member(t, o.ID, u, model.OrgRoleMember)

	err := f.os.AddMember(context.Background(), o.ID, u, model.OrgRoleOwner)
	wantUniqueViolation(t, "re-adding a member", err)
}

func TestOrgStore_LastOwnerProtected(t *testing.T) {
	f := newOrgFixture(t)
	ctx := context.Background()
	o := f.org(t, "owners")
	owner1, owner2, plain := f.user(t, "o1"), f.user(t, "o2"), f.user(t, "m")
	f.member(t, o.ID, owner1, model.OrgRoleOwner)
	f.member(t, o.ID, plain, model.OrgRoleMember)

	if err := f.os.RemoveMember(ctx, o.ID, owner1); !errors.Is(err, store.ErrLastOrgOwner) {
		t.Errorf("RemoveMember last owner: %v, want ErrLastOrgOwner", err)
	}
	if err := f.os.UpdateMemberRole(ctx, o.ID, owner1, model.OrgRoleMember); !errors.Is(err, store.ErrLastOrgOwner) {
		t.Errorf("demote last owner: %v, want ErrLastOrgOwner", err)
	}
	if m, err := f.os.GetMember(ctx, o.ID, owner1); err != nil || m.Role != model.OrgRoleOwner {
		t.Errorf("owner after refused changes = %+v, %v", m, err)
	}

	if err := f.os.UpdateMemberRole(ctx, o.ID, plain, model.OrgRoleOwner); err != nil {
		t.Fatalf("promote: %v", err)
	}
	if err := f.os.UpdateMemberRole(ctx, o.ID, owner1, model.OrgRoleMember); err != nil {
		t.Errorf("demote with another owner present: %v", err)
	}
	if err := f.os.RemoveMember(ctx, o.ID, plain); !errors.Is(err, store.ErrLastOrgOwner) {
		t.Errorf("RemoveMember now-last owner: %v, want ErrLastOrgOwner", err)
	}

	f.member(t, o.ID, owner2, model.OrgRoleOwner)
	if err := f.os.RemoveMember(ctx, o.ID, plain); err != nil {
		t.Errorf("remove owner when another remains: %v", err)
	}
	if err := f.os.RemoveMember(ctx, o.ID, owner1); err != nil {
		t.Errorf("remove plain member: %v", err)
	}
	if _, err := f.os.GetMember(ctx, o.ID, owner1); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetMember after remove: %v, want sql.ErrNoRows", err)
	}
}

func TestOrgStore_RemoveMember_NonMember(t *testing.T) {
	f := newOrgFixture(t)
	o := f.org(t, "nonmember")
	if err := f.os.RemoveMember(context.Background(), o.ID, f.user(t, "u")); err != nil {
		t.Errorf("removing a non-member: %v", err)
	}
}

func TestOrgStore_CountMembers(t *testing.T) {
	f := newOrgFixture(t)
	ctx := context.Background()
	o := f.org(t, "count")

	if n, err := f.os.CountMembers(ctx, o.ID); err != nil || n != 0 {
		t.Fatalf("empty org = %d, %v", n, err)
	}
	f.member(t, o.ID, f.user(t, "a"), model.OrgRoleOwner)
	f.member(t, o.ID, f.user(t, "b"), model.OrgRoleMember)
	if n, err := f.os.CountMembers(ctx, o.ID); err != nil || n != 2 {
		t.Errorf("CountMembers = %d, %v; want 2", n, err)
	}
}

func TestOrgStore_ListByMember(t *testing.T) {
	f := newOrgFixture(t)
	ctx := context.Background()
	zed, alpha, notMine := f.org(t, "zed"), f.org(t, "alpha"), f.org(t, "other")
	u := f.user(t, "u")
	f.member(t, zed.ID, u, model.OrgRoleMember)
	f.member(t, alpha.ID, u, model.OrgRoleOwner)
	f.member(t, notMine.ID, f.user(t, "v"), model.OrgRoleOwner)

	got, err := f.os.ListByMember(ctx, u)
	if err != nil {
		t.Fatalf("ListByMember: %v", err)
	}
	var names []string
	for _, o := range got {
		names = append(names, o.Name)
	}
	if want := []string{alpha.Name, zed.Name}; !slices.Equal(names, want) {
		t.Errorf("orgs = %v, want %v (by name, member orgs only)", names, want)
	}

	if got, err := f.os.ListByMember(ctx, f.user(t, "loner")); err != nil || len(got) != 0 {
		t.Errorf("non-member = %v, %v; want empty", got, err)
	}
}

func TestOrgStore_SwapAvatarKey(t *testing.T) {
	f := newOrgFixture(t)
	ctx := context.Background()
	o := f.org(t, "avatar")

	old, err := f.os.SwapAvatarKey(ctx, o.ID, "k1")
	if err != nil || old != "" {
		t.Fatalf("first swap = %q, %v; want empty, nil", old, err)
	}
	old, err = f.os.SwapAvatarKey(ctx, o.ID, "k2")
	if err != nil || old != "k1" {
		t.Fatalf("second swap = %q, %v; want k1, nil", old, err)
	}
	if got, _ := f.os.GetByID(ctx, o.ID); got.AvatarKey != "k2" {
		t.Errorf("AvatarKey = %q, want k2", got.AvatarKey)
	}
	if _, err := f.os.SwapAvatarKey(ctx, o.ID+1_000_000, "x"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("unknown org: %v, want sql.ErrNoRows", err)
	}
}

func TestOrgStore_Delete(t *testing.T) {
	f := newOrgFixture(t)
	ctx := context.Background()

	t.Run("refused while a live repo remains", func(t *testing.T) {
		o := f.org(t, "live")
		f.orgRepo(t, o, "live", false)

		_, _, err := f.os.Delete(ctx, o.ID)
		if !errors.Is(err, store.ErrOrgHasRepos) {
			t.Fatalf("Delete: %v, want ErrOrgHasRepos", err)
		}
		if _, err := f.os.GetByID(ctx, o.ID); err != nil {
			t.Errorf("org removed despite refusal: %v", err)
		}
	})

	t.Run("returns soft-deleted repos and the avatar key", func(t *testing.T) {
		o := f.org(t, "gone")
		if _, err := f.os.SwapAvatarKey(ctx, o.ID, "avatar-key"); err != nil {
			t.Fatalf("SwapAvatarKey: %v", err)
		}
		repoID := f.orgRepo(t, o, "soft", true)

		deleted, key, err := f.os.Delete(ctx, o.ID)
		if err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if key != "avatar-key" {
			t.Errorf("avatarKey = %q", key)
		}
		if len(deleted) != 1 || deleted[0].ID != repoID || deleted[0].OrgID != o.ID || deleted[0].DeletedAt == nil || deleted[0].OwnerName != o.Name {
			t.Errorf("deleted = %+v", deleted)
		}
		if _, err := f.os.GetByID(ctx, o.ID); !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("org still present: %v", err)
		}
		var n int
		if err := f.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM repositories WHERE id = $1`, repoID).Scan(&n); err != nil || n != 0 {
			t.Errorf("soft-deleted repo rows after cascade = %d, %v; want 0", n, err)
		}
	})

	t.Run("empty org", func(t *testing.T) {
		o := f.org(t, "empty")
		deleted, key, err := f.os.Delete(ctx, o.ID)
		if err != nil || len(deleted) != 0 || key != "" {
			t.Errorf("Delete = %v, %q, %v", deleted, key, err)
		}
	})

	t.Run("unknown org", func(t *testing.T) {
		if _, _, err := f.os.Delete(ctx, -1); !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("Delete unknown: %v, want sql.ErrNoRows", err)
		}
	})
}
