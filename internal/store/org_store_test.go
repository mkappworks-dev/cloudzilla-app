package store_test

// Integration tests for OrgStore. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// TestOrgStore_Create_AssignsID verifies that Create inserts an organization row
// and returns a non-zero ID assigned by the database.
func TestOrgStore_Create_AssignsID(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	os := store.NewOrgStore(db)

	org := &model.Organization{
		Name:        "testorg_" + suffix,
		DisplayName: "Test Org",
	}
	if err := os.Create(context.Background(), org); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if org.ID == 0 {
		t.Error("Create must assign a non-zero ID")
	}
}

// TestOrgStore_GetByName_ReturnsOrg verifies that GetByName finds the org by its
// exact name after creation.
func TestOrgStore_GetByName_ReturnsOrg(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	os := store.NewOrgStore(db)

	org := &model.Organization{Name: "findable_" + suffix, DisplayName: "Findable"}
	if err := os.Create(context.Background(), org); err != nil {
		t.Fatalf("Create: %v", err)
	}

	found, err := os.GetByName(context.Background(), "findable_"+suffix)
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if found.ID != org.ID {
		t.Errorf("want org ID %d, got %d", org.ID, found.ID)
	}
}

// TestOrgStore_GetByName_Unknown_Error verifies that GetByName returns an error
// when no organization with the given name exists.
func TestOrgStore_GetByName_Unknown_Error(t *testing.T) {
	db := testutil.OpenTestDB(t)
	os := store.NewOrgStore(db)

	_, err := os.GetByName(context.Background(), "does_not_exist_org")
	if err == nil {
		t.Error("GetByName must return an error for an unknown org name")
	}
}

// TestOrgStore_AddMember_ThenGetMember verifies that AddMember inserts a member row
// and GetMember returns it with the correct role.
func TestOrgStore_AddMember_ThenGetMember(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	os := store.NewOrgStore(db)
	memberID := testutil.SeedUser(t, db, "m_"+suffix)

	org := &model.Organization{Name: "org_member_" + suffix, DisplayName: "Org"}
	if err := os.Create(context.Background(), org); err != nil {
		t.Fatalf("Create org: %v", err)
	}

	if err := os.AddMember(context.Background(), org.ID, memberID, model.OrgRoleMember); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	m, err := os.GetMember(context.Background(), org.ID, memberID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if m.Role != model.OrgRoleMember {
		t.Errorf("want role %q, got %q", model.OrgRoleMember, m.Role)
	}
}

// TestOrgStore_RemoveMember_DeletesRow verifies that RemoveMember deletes the
// membership row so that GetMember returns an error afterward.
func TestOrgStore_RemoveMember_DeletesRow(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	os := store.NewOrgStore(db)
	memberID := testutil.SeedUser(t, db, "rm_"+suffix)

	org := &model.Organization{Name: "org_rm_" + suffix, DisplayName: "Org"}
	if err := os.Create(context.Background(), org); err != nil {
		t.Fatalf("Create org: %v", err)
	}
	if err := os.AddMember(context.Background(), org.ID, memberID, model.OrgRoleMember); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if err := os.RemoveMember(context.Background(), org.ID, memberID); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}

	_, err := os.GetMember(context.Background(), org.ID, memberID)
	if err == nil {
		t.Error("GetMember must return an error after the member is removed")
	}
}

// TestOrgStore_UpdateMemberRole verifies that UpdateMemberRole changes an existing
// member's role from member to owner and the change is persisted.
func TestOrgStore_UpdateMemberRole(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	os := store.NewOrgStore(db)
	memberID := testutil.SeedUser(t, db, "role_"+suffix)

	org := &model.Organization{Name: "org_role_" + suffix, DisplayName: "Org"}
	if err := os.Create(context.Background(), org); err != nil {
		t.Fatalf("Create org: %v", err)
	}
	if err := os.AddMember(context.Background(), org.ID, memberID, model.OrgRoleMember); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if err := os.UpdateMemberRole(context.Background(), org.ID, memberID, model.OrgRoleOwner); err != nil {
		t.Fatalf("UpdateMemberRole: %v", err)
	}

	m, err := os.GetMember(context.Background(), org.ID, memberID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if m.Role != model.OrgRoleOwner {
		t.Errorf("want role owner after promotion, got %q", m.Role)
	}
}

// TestOrgStore_ListMembers_ReturnsAllMembers verifies that ListMembers returns all
// members added to the organization.
func TestOrgStore_ListMembers_ReturnsAllMembers(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	os := store.NewOrgStore(db)
	m1 := testutil.SeedUser(t, db, "lm1_"+suffix)
	m2 := testutil.SeedUser(t, db, "lm2_"+suffix)

	org := &model.Organization{Name: "org_list_" + suffix, DisplayName: "Org"}
	if err := os.Create(context.Background(), org); err != nil {
		t.Fatalf("Create org: %v", err)
	}
	for _, uid := range []int64{m1, m2} {
		if err := os.AddMember(context.Background(), org.ID, uid, model.OrgRoleMember); err != nil {
			t.Fatalf("AddMember: %v", err)
		}
	}

	members, err := os.ListMembers(context.Background(), org.ID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) < 2 {
		t.Errorf("want at least 2 members, got %d", len(members))
	}
}
