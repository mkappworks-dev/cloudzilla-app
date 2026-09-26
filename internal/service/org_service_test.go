package service_test

// Integration tests for OrgService. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

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

func TestOrgService_RemoveMember_MemberCanLeaveSelf(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	creatorID := testutil.SeedUser(t, db, suffix)
	memberID := testutil.SeedUser(t, db, "leaver_"+suffix)

	svc := service.NewOrgService(
		store.NewOrgStore(db),
		store.NewRepoStore(db),
		store.NewUserStore(db),
		config.GitConfig{},
	)

	org, err := svc.Create(context.Background(), creatorID, "testorg_leave_"+suffix, "Org", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.AddMember(context.Background(), org.ID, creatorID, memberID, model.OrgRoleMember); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	if err := svc.RemoveMember(context.Background(), org.ID, memberID, memberID); err != nil {
		t.Fatalf("member leaving themselves: %v", err)
	}
	if svc.IsMember(context.Background(), org.ID, memberID) {
		t.Error("member must no longer belong to the org after leaving")
	}

	if err := svc.RemoveMember(context.Background(), org.ID, creatorID, creatorID); err == nil {
		t.Error("sole owner must not be able to leave (last-owner guard)")
	}
}

func TestOrgService_CountMembers(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	creatorID := testutil.SeedUser(t, db, suffix)

	svc := service.NewOrgService(
		store.NewOrgStore(db),
		store.NewRepoStore(db),
		store.NewUserStore(db),
		config.GitConfig{},
	)

	org, err := svc.Create(context.Background(), creatorID, "testorg_count_"+suffix, "Org", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Creator is auto-added as owner member.
	count, err := svc.CountMembers(context.Background(), org.ID)
	if err != nil {
		t.Fatalf("CountMembers: %v", err)
	}
	if count != 1 {
		t.Errorf("want 1 member after Create, got %d", count)
	}

	memberID := testutil.SeedUser(t, db, "counted_"+suffix)
	if err := svc.AddMember(context.Background(), org.ID, creatorID, memberID, model.OrgRoleMember); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	count, err = svc.CountMembers(context.Background(), org.ID)
	if err != nil {
		t.Fatalf("CountMembers: %v", err)
	}
	if count != 2 {
		t.Errorf("want 2 members after AddMember, got %d", count)
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

func TestOrgService_CreateRepo_WithInitFiles(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	creatorID := testutil.SeedUser(t, db, suffix)
	root := t.TempDir()

	svc := service.NewOrgService(
		store.NewOrgStore(db),
		store.NewRepoStore(db),
		store.NewUserStore(db),
		config.GitConfig{ReposRoot: root},
	)

	ctx := context.Background()
	org, err := svc.Create(ctx, creatorID, "testorg_initrepo_"+suffix, "Org", "")
	if err != nil {
		t.Fatalf("Create org: %v", err)
	}

	repo, err := svc.CreateRepo(ctx, org.ID, creatorID, "initrepo", "an initialized project", false, service.RepoInitOptions{
		AddREADME: true,
		Gitignore: "Go",
		License:   "mit",
	})
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	bareDir := filepath.Join(root, org.Name, repo.Name+".git")
	files := bareTreeFiles(t, bareDir, repo.DefaultBranch)

	for _, want := range []string{"README.md", ".gitignore", "LICENSE"} {
		if !files[want] {
			t.Errorf("initial commit missing %s (have %v)", want, files)
		}
	}
}

// The website renders as a clickable link on the public org page, so a
// javascript: URL saved here would run in every visitor's browser.
func TestOrgService_UpdateProfile_RejectsNonHTTPWebsite(t *testing.T) {
	svc, ownerID := newOrgSvc(t)
	ctx := context.Background()
	org, err := svc.Create(ctx, ownerID, "testorg_"+testutil.UniqueSuffix(t), "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	for _, website := range []string{"javascript:alert(1)", "JavaScript:alert(1)", "data:text/html,hi", "https://"} {
		if err := svc.UpdateProfile(ctx, org.ID, ownerID, "", "", website, "", ""); err == nil {
			t.Errorf("UpdateProfile(website=%q) = nil error, want rejection", website)
		}
	}
	got, err := svc.Get(ctx, org.Name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Website != "" {
		t.Errorf("stored website = %q after rejected updates, want empty", got.Website)
	}
}

// A bare host would otherwise render as a relative link to /{host}.
func TestOrgService_UpdateProfile_BareHostGetsHTTPS(t *testing.T) {
	svc, ownerID := newOrgSvc(t)
	ctx := context.Background()
	org, err := svc.Create(ctx, ownerID, "testorg_"+testutil.UniqueSuffix(t), "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.UpdateProfile(ctx, org.ID, ownerID, "", "", "acme.dev", "", ""); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	got, err := svc.Get(ctx, org.Name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Website != "https://acme.dev" {
		t.Errorf("stored website = %q, want %q", got.Website, "https://acme.dev")
	}
}

func TestOrgService_RepoHighlights(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	stargazers := make([]int64, 5)
	for i := range stargazers {
		stargazers[i] = testutil.SeedUser(t, db, fmt.Sprintf("%s_star%d", suffix, i))
	}

	now := time.Now()
	specs := []struct {
		name    string
		private bool
		stars   int
		age     time.Duration
	}{
		{"popular", false, 2, 3 * time.Hour},
		{"fresh", false, 0, 1 * time.Hour},
		{"secret", true, 5, 0},
		{"liked", false, 1, 2 * time.Hour},
		{"stale", false, 0, 4 * time.Hour},
	}
	repos := make([]model.Repository, len(specs))
	for i, sp := range specs {
		id := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix+"_"+sp.name)
		for _, uid := range stargazers[:sp.stars] {
			if _, err := db.ExecContext(ctx, `INSERT INTO stars (user_id, repo_id) VALUES ($1, $2)`, uid, id); err != nil {
				t.Fatalf("star %s: %v", sp.name, err)
			}
		}
		repos[i] = model.Repository{ID: id, Name: sp.name, Private: sp.private, UpdatedAt: now.Add(-sp.age)}
	}

	svc := service.NewOrgService(store.NewOrgStore(db), store.NewRepoStore(db), store.NewUserStore(db), config.GitConfig{}).
		WithStarStore(store.NewStarStore(db))
	cards := func(rs []model.RepositoryWithStats) []string {
		out := make([]string, len(rs))
		for i, r := range rs {
			out[i] = fmt.Sprintf("%s:%d", r.Name, r.StarCount)
		}
		return out
	}

	got, err := svc.RepoHighlights(ctx, repos, 2, 2)
	if err != nil {
		t.Fatalf("RepoHighlights: %v", err)
	}
	if want := []string{"popular:2", "liked:1"}; !reflect.DeepEqual(cards(got.Featured), want) {
		t.Errorf("featured = %v, want %v (most-starred public repos)", cards(got.Featured), want)
	}
	if want := []string{"secret:5", "fresh:0"}; !reflect.DeepEqual(cards(got.Recent), want) {
		t.Errorf("recent = %v, want %v (newest first, featured skipped)", cards(got.Recent), want)
	}

	all, err := svc.RepoHighlights(ctx, repos, 0, len(repos))
	if err != nil {
		t.Fatalf("RepoHighlights all: %v", err)
	}
	if len(all.Featured) != 0 {
		t.Errorf("featured with limit 0 = %v, want none", cards(all.Featured))
	}
	if want := []string{"secret:5", "fresh:0", "liked:1", "popular:2", "stale:0"}; !reflect.DeepEqual(cards(all.Recent), want) {
		t.Errorf("all recent = %v, want %v", cards(all.Recent), want)
	}
}

func TestOrgService_ListMembershipsForUser_OwnedAndMember(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	aliceID := testutil.SeedUser(t, db, "alice_"+suffix)
	bobID := testutil.SeedUser(t, db, "bob_"+suffix)
	carolID := testutil.SeedUser(t, db, "carol_"+suffix)

	svc := service.NewOrgService(
		store.NewOrgStore(db),
		store.NewRepoStore(db),
		store.NewUserStore(db),
		config.GitConfig{},
	)
	ctx := context.Background()

	ownedOrg, err := svc.Create(ctx, aliceID, "testorg_owned_"+suffix, "", "")
	if err != nil {
		t.Fatalf("Create owned org: %v", err)
	}
	memberOrg, err := svc.Create(ctx, bobID, "testorg_member_"+suffix, "", "")
	if err != nil {
		t.Fatalf("Create member org: %v", err)
	}
	if err := svc.AddMember(ctx, memberOrg.ID, bobID, aliceID, model.OrgRoleMember); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	unrelatedOrg, err := svc.Create(ctx, carolID, "testorg_unrelated_"+suffix, "", "")
	if err != nil {
		t.Fatalf("Create unrelated org: %v", err)
	}

	memberships, err := svc.ListMembershipsForUser(ctx, aliceID)
	if err != nil {
		t.Fatalf("ListMembershipsForUser: %v", err)
	}
	roles := map[int64]model.OrgRole{}
	for _, m := range memberships {
		roles[m.Org.ID] = m.Role
	}
	if len(roles) != 2 {
		t.Fatalf("want 2 orgs (owned + member), got %d: %+v", len(roles), memberships)
	}
	if got := roles[ownedOrg.ID]; got != model.OrgRoleOwner {
		t.Errorf("owned org role = %q, want %q", got, model.OrgRoleOwner)
	}
	if got := roles[memberOrg.ID]; got != model.OrgRoleMember {
		t.Errorf("member org role = %q, want %q", got, model.OrgRoleMember)
	}
	if _, ok := roles[unrelatedOrg.ID]; ok {
		t.Errorf("unrelated org %q must not be listed", unrelatedOrg.Name)
	}

	owned, err := svc.ListOwnedByUser(ctx, aliceID)
	if err != nil {
		t.Fatalf("ListOwnedByUser: %v", err)
	}
	if len(owned) != 1 || owned[0].ID != ownedOrg.ID {
		t.Errorf("ListOwnedByUser = %+v, want only %q", owned, ownedOrg.Name)
	}

	if c, err := svc.CountMembers(ctx, memberOrg.ID); err != nil || c != 2 {
		t.Errorf("CountMembers(member org) = %d, err=%v; want 2", c, err)
	}
}
