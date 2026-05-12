package service_test

// Integration tests for OrgService. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// newOrgSvc builds an OrgService backed by the test database.
// Returns the service and a seeded creator user ID.
func newOrgSvc(t *testing.T) (*service.OrgService, int64) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	creatorID := testutil.SeedUser(t, db, suffix)
	svc := service.NewOrgService(
		store.NewOrgStore(db),
		store.NewRepoStore(db),
		store.NewUserStore(db),
		config.GitConfig{},
	)
	return svc, creatorID
}

// TestOrgService_Create_AssignsIDAndOwner verifies that Create inserts the org, returns
// a non-zero ID, and automatically makes the creator an owner member.
func TestOrgService_Create_AssignsIDAndOwner(t *testing.T) {
	svc, creatorID := newOrgSvc(t)
	suffix := testutil.UniqueSuffix(t)
	orgName := "testorg_" + suffix

	org, err := svc.Create(context.Background(), creatorID, orgName, "Test Org", "desc")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if org.ID == 0 {
		t.Error("created org must have non-zero ID")
	}
	if org.Name != orgName {
		t.Errorf("want org name %q, got %q", orgName, org.Name)
	}

	// Creator must be an owner member automatically.
	if !svc.IsOwner(context.Background(), org.ID, creatorID) {
		t.Error("creator must be owner after org creation")
	}
}

// TestOrgService_Create_NameConflictWithUser_Error verifies that an org cannot be
// created with the same name as an existing user (namespace collision prevention).
func TestOrgService_Create_NameConflictWithUser_Error(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	creatorID := testutil.SeedUser(t, db, suffix)
	// The seeded user's username is "testuser_<suffix>".
	existingUsername := "testuser_" + suffix

	svc := service.NewOrgService(
		store.NewOrgStore(db),
		store.NewRepoStore(db),
		store.NewUserStore(db),
		config.GitConfig{},
	)

	_, err := svc.Create(context.Background(), creatorID, existingUsername, "Conflict Org", "")
	if err == nil {
		t.Error("Create must fail when org name conflicts with an existing username")
	}
}

// TestOrgService_AddMember_OwnerCanAdd verifies that an org owner can add another user
// as a member with the "member" role.
func TestOrgService_AddMember_OwnerCanAdd(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	creatorID := testutil.SeedUser(t, db, suffix)
	newMemberID := testutil.SeedUser(t, db, "member_"+suffix)

	svc := service.NewOrgService(
		store.NewOrgStore(db),
		store.NewRepoStore(db),
		store.NewUserStore(db),
		config.GitConfig{},
	)

	org, err := svc.Create(context.Background(), creatorID, "testorg_add_"+suffix, "Org", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.AddMember(context.Background(), org.ID, creatorID, newMemberID, model.OrgRoleMember); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	// Confirm membership.
	if !svc.IsMember(context.Background(), org.ID, newMemberID) {
		t.Error("added user must be a member of the org")
	}
}

// TestOrgService_AddMember_NonOwnerDenied verifies that a non-owner cannot add members
// to an organization (only owners control membership).
func TestOrgService_AddMember_NonOwnerDenied(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	creatorID := testutil.SeedUser(t, db, suffix)
	nonOwnerID := testutil.SeedUser(t, db, "nonowner_"+suffix)
	targetID := testutil.SeedUser(t, db, "target_"+suffix)

	svc := service.NewOrgService(
		store.NewOrgStore(db),
		store.NewRepoStore(db),
		store.NewUserStore(db),
		config.GitConfig{},
	)

	org, err := svc.Create(context.Background(), creatorID, "testorg_deny_"+suffix, "Org", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// nonOwner is not a member at all — AddMember must be denied.
	err = svc.AddMember(context.Background(), org.ID, nonOwnerID, targetID, model.OrgRoleMember)
	if err == nil {
		t.Error("non-owner must not be able to add members")
	}
}

// TestOrgService_RemoveMember_OwnerCanRemove verifies that an org owner can remove
// a member from the organization.
func TestOrgService_RemoveMember_OwnerCanRemove(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	creatorID := testutil.SeedUser(t, db, suffix)
	memberID := testutil.SeedUser(t, db, "removable_"+suffix)

	svc := service.NewOrgService(
		store.NewOrgStore(db),
		store.NewRepoStore(db),
		store.NewUserStore(db),
		config.GitConfig{},
	)

	org, err := svc.Create(context.Background(), creatorID, "testorg_rm_"+suffix, "Org", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Add then remove.
	if err := svc.AddMember(context.Background(), org.ID, creatorID, memberID, model.OrgRoleMember); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if err := svc.RemoveMember(context.Background(), org.ID, creatorID, memberID); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}

	if svc.IsMember(context.Background(), org.ID, memberID) {
		t.Error("removed user must no longer be a member")
	}
}

// TestOrgService_IsOwner_MemberRole_ReturnsFalse verifies that a user with the "member"
// role is not considered an owner.
func TestOrgService_IsOwner_MemberRole_ReturnsFalse(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	creatorID := testutil.SeedUser(t, db, suffix)
	memberID := testutil.SeedUser(t, db, "justmember_"+suffix)

	svc := service.NewOrgService(
		store.NewOrgStore(db),
		store.NewRepoStore(db),
		store.NewUserStore(db),
		config.GitConfig{},
	)

	org, err := svc.Create(context.Background(), creatorID, "testorg_iso_"+suffix, "Org", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.AddMember(context.Background(), org.ID, creatorID, memberID, model.OrgRoleMember); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	if svc.IsOwner(context.Background(), org.ID, memberID) {
		t.Error("user with member role must not be IsOwner")
	}
}
